// This is a simple X11 application that displays image and supports WM_DELETE_WINDOW
package main

import (
	"encoding/hex"
	"errors"
	"io"
	"net"
	"syscall"
	"unsafe"

	"github.com/dzeromsk/helloX11/x11byte"
	"golang.org/x/sys/unix"
)

const (
	width  = 1024
	height = 1024
)

func main() {
	conn, err := net.Dial("unix", "/tmp/.X11-unix/X0")
	// conn, err := net.Dial("tcp4", "127.0.0.1:6000")
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	nextID, parentID, visualID, err := authenticate(conn)
	if err != nil {
		panic(err)
	}
	_ = visualID

	// TODO(dzeromsk): verify wmProtocols and wmDeleteWindow are static
	// wmProtocols, wmDeleteWindow, err := internAtom(conn)
	// if err != nil {
	// 	panic(err)
	// }
	wmProtocols, wmDeleteWindow := uint32(441), uint32(439)
	// println(wmProtocols, wmDeleteWindow)

	// presentOpcode, _ := presentExtension(conn)
	presentOpcode := uint8(148)
	println("present:", presentOpcode)

	// dri3Opcode, _ := dri3Extension(conn)
	dri3Opcode := uint8(149)
	println("dri3:", dri3Opcode)

	pixmapID := nextID()
	pfd, err := pixmap(conn, pixmapID, parentID, dri3Opcode)
	if err != nil {
		panic(err)
	}

	pdata, err := unix.Mmap(int(pfd), 0, width*height*4, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		panic(err)
	}
	defer unix.Munmap(pdata)

	var sync dma_buf_sync
	sync.flags = DMA_BUF_SYNC_START | DMA_BUF_SYNC_WRITE
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(pfd), DMA_BUF_IOCTL_SYNC, uintptr(unsafe.Pointer(&sync)))
	if errno != 0 {
		panic(errno)
	}

	for i := range pdata {
		pdata[i] = 0xff
	}

	// 128x8=4096(1page)

	fill := func(block int) {
		offset := block * 4096
		for y := 0; y < 8*4; y += 4 {
			for x := 0; x < 128*4; x += 4 {
				pdata[offset+y*128+x+0] = 0x00
				pdata[offset+y*128+x+1] = 0x00
				pdata[offset+y*128+x+2] = 0x00

			}
		}
	}

	for by := range 128 {
		for bx := range 8 {
			if bx != 2 {
				continue
			}
			if by < 64 || by > 78 {
				continue
			}
			block := by*8 + bx
			fill(block)
		}
	}

	sync.flags = DMA_BUF_SYNC_END | DMA_BUF_SYNC_WRITE
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(pfd), DMA_BUF_IOCTL_SYNC, uintptr(unsafe.Pointer(&sync)))
	if errno != 0 {
		panic(errno)
	}

	var b x11byte.Builder

	windowID := nextID()

	// Create Window, required
	b.AddUint8(X11_REQUEST_CREATE_WINDOW) // opcode
	b.AddUint8(0)                         // depth
	b.AddUint16(10)                       // requestLength
	b.AddUint32(windowID)                 // windowID
	b.AddUint32(parentID)                 // parent
	b.AddUint16(0)                        // x
	b.AddUint16(0)                        // y
	b.AddUint16(width)                    // width
	b.AddUint16(height)                   // height
	b.AddUint16(1)                        // borderWidth
	// b.AddUint16(WINDOWCLASS_INPUTOUTPUT)                     // windowClass
	// b.AddUint32(visualID)                                    // visualID
	b.AddUint16(0)                                              // windowClass
	b.AddUint32(0)                                              // visualID
	b.AddUint32(X11_FLAG_BACKGROUND_PIXEL | X11_FLAG_WIN_EVENT) // windowValueMask
	b.AddUint32(0x00000000)                                     // backgroundPixel
	b.AddUint32(X11_EVENT_FLAG_EXPOSURE)                        // windowEvents

	// Change Property, to let know WM that we support delete window
	b.AddUint8(X11_REQUEST_CHANGE_PROPERTY) // opcode
	b.AddUint8(0)                           // mode
	b.AddUint16(7)                          // requestLength
	b.AddUint32(windowID)                   // windowID
	b.AddUint32(wmProtocols)                // property
	b.AddUint32(4)                          // type
	b.AddUint8(32)                          // format
	b.AddUint24(0)                          // unused
	b.AddUint32(1)                          // dataLength
	b.AddUint32(wmDeleteWindow)             // data

	var serial uint32

	// Present Pixmap
	b.AddUint8(presentOpcode) // opcode
	b.AddUint8(1)             // extension-minor, present
	b.AddUint16(0x12)         // requestLength
	b.AddUint32(windowID)     // window
	b.AddUint32(pixmapID)     // pixmap
	b.AddUint32(serial)       // serial
	b.AddUint32(0)            // valid
	b.AddUint32(0)            // update
	b.AddUint16(0)            // x_off
	b.AddUint16(0)            // y_off
	b.AddUint32(0)            // target_crtc
	b.AddUint32(0)            // wait_fence
	b.AddUint32(0)            // idle_fence
	b.AddUint32(8)            // options
	b.AddUint32(0)            // unused
	// b.AddUint32(0x0356a519) // target_msc
	b.AddUint32(0) // target_msc
	b.AddUint32(0) // target_msc
	b.AddUint32(0) // divisor
	b.AddUint32(0) // divisor
	b.AddUint32(0) // remainder
	b.AddUint32(0) // remainder

	// Map window, required
	b.AddUint8(X11_REQUEST_MAP_WINDOW) // opcode
	b.AddUint8(0)                      // unused
	b.AddUint16(2)                     // requestLength
	b.AddUint32(windowID)              // windowID

	if _, err := conn.Write(b.BytesOrPanic()); err != nil {
		panic(err)
	}

	data := x11byte.String(make([]byte, 4096))
recv:
	for {
		n, err := conn.Read(data)
		if err != nil {
			panic(err)
		}
		if n == 0 {
			break
		}

		msg := data[:n]
		switch msg[0] {
		case 0:
			println("response error")
			print(hex.Dump(msg))

		case 1:
			println("reply ok")
			print(hex.Dump(msg))

		default:
			// println("event")

			switch msg[0] {
			case 12: // Expose event, need to redraw
				msg.Skip(1) // evetcode
				msg.Skip(1) // unused
				msg.Skip(2) // sequenceNumber
				msg.Skip(4) // window
				var x, y, w, h uint16
				msg.ReadUint16(&x)
				msg.ReadUint16(&y)
				msg.ReadUint16(&w)
				msg.ReadUint16(&h)
				// println(x, y, w, h)

				var r x11byte.Builder

				// Present Pixmap
				r.AddUint8(presentOpcode) // opcode
				r.AddUint8(1)             // extension-minor, present
				r.AddUint16(0x12)         // requestLength
				r.AddUint32(windowID)     // window
				r.AddUint32(pixmapID)     // pixmap
				r.AddUint32(serial)       // serial
				r.AddUint32(0)            // valid
				r.AddUint32(0)            // update
				r.AddUint16(0)            // x_off
				r.AddUint16(0)            // y_off
				r.AddUint32(0)            // target_crtc
				r.AddUint32(0)            // wait_fence
				r.AddUint32(0)            // idle_fence
				r.AddUint32(8)            // options
				r.AddUint32(0)            // unused
				// r.AddUint32(0x0356a519) // target_msc
				r.AddUint32(0) // target_msc
				r.AddUint32(0) // target_msc
				r.AddUint32(0) // divisor
				r.AddUint32(0) // divisor
				r.AddUint32(0) // remainder
				r.AddUint32(0) // remainder

				present := r.BytesOrPanic()
				if _, err := conn.Write(present); err != nil {
					panic(err)
				}

				serial++

			case 161: // Client Message, maybe vmDeleteWindow from Window Manager
				msg.Skip(1) // eventCode
				msg.Skip(1) // format
				msg.Skip(2) // sequenceNumber
				msg.Skip(4) // window
				var typ, value uint32
				msg.ReadUint32(&typ)
				msg.ReadUint32(&value)
				// println(typ, value)
				if typ == wmProtocols && value == wmDeleteWindow {
					break recv
				}
			default:
				print(hex.Dump(msg))
			}
		}
	}
}

const (
	X11_FLAG_WIN_EVENT        = 0x00000800
	X11_FLAG_BACKGROUND_PIXEL = 0x00000002
)

const (
	WINDOWCLASS_COPYFROMPARENT = 0
	WINDOWCLASS_INPUTOUTPUT    = 1
	WINDOWCLASS_INPUTONLY      = 2
)

const (
	X11_EVENT_FLAG_KEY_PRESS   = 0x00000001
	X11_EVENT_FLAG_KEY_RELEASE = 0x00000002
	X11_EVENT_FLAG_EXPOSURE    = 0x8000
)

const (
	X11_GC_FLAG_BACKGROUND = 0x00000008
)

const (
	X11_REQUEST_CREATE_WINDOW   = 1
	X11_REQUEST_MAP_WINDOW      = 8
	X11_REQUEST_INTERN_ATOM     = 16
	X11_REQUEST_CHANGE_PROPERTY = 18
	X11_REQUEST_CREATE_PIXMAP   = 53
	X11_REQUEST_CREATE_GC       = 55
	X11_REQUEST_QUERY_EXTENSION = 98
)

var (
	errResponseStateFailed       = errors.New("RESPONSE_STATE_FAILED")
	errResponseStateAuthenticate = errors.New("RESPONSE_STATE_AUTHENTICATE")
	errResponseStateUnknown      = errors.New("RESPONSE_STATE_UNKNOWN")
)

func authenticate(conn net.Conn) (func() uint32, uint32, uint32, error) {
	var b x11byte.Builder
	b.AddUint8('l') // byte-order
	b.AddUint8(0)   // unused
	b.AddUint16(11) // protocol-major-version
	b.AddUint16(0)  // protocol-minor-version
	b.AddUint16(0)  // authorization-protocol-name-length
	b.AddUint16(0)  // authorization-protocol-data-length
	b.AddUint16(0)  // protocol-minor-version
	if _, err := conn.Write(b.BytesOrPanic()); err != nil {
		return nil, 0, 0, err
	}

	header := x11byte.String(make([]byte, 8))
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, 0, 0, err
	}

	var (
		status       uint8
		replayLength uint16
	)
	header.ReadUint8(&status)
	header.Skip(1) // unused
	header.Skip(2) // majorVersion
	header.Skip(2) // minorVersion
	header.ReadUint16(&replayLength)

	switch status {
	case 0:
		return nil, 0, 0, errResponseStateFailed
	case 1:
		// RESPONSE_STATE_SUCCESS
	case 2:
		return nil, 0, 0, errResponseStateAuthenticate
	default:
		return nil, 0, 0, errResponseStateUnknown
	}

	replay := x11byte.String(make([]byte, replayLength*4))
	if _, err := io.ReadFull(conn, replay); err != nil {
		return nil, 0, 0, err
	}

	var (
		resourceIDBase  uint32
		resourceIDMask  uint32
		lengthOfVendor  uint16
		numberOfFormats uint8
	)
	replay.Skip(4) // releaseNumber
	replay.ReadUint32(&resourceIDBase)
	replay.ReadUint32(&resourceIDMask)
	replay.Skip(4) // motionBufferSize
	replay.ReadUint16(&lengthOfVendor)
	replay.Skip(2) // maximumRequestLength
	replay.Skip(1) // numberOfScreensInRoot
	replay.ReadUint8(&numberOfFormats)
	replay.Skip(1) // imageByteOrder
	replay.Skip(1) // bitmapFormatByteOrder
	replay.Skip(1) // bitmapFormatScanlineUnit
	replay.Skip(1) // bitmapFormatScanlinePad
	replay.Skip(1) // minKeycode
	replay.Skip(1) // maxKeyCode
	replay.Skip(4) // unused

	// skip large arrays
	replay.Skip(int(lengthOfVendor))                 // vendor
	replay.Skip(int((4 - (lengthOfVendor % 4)) % 4)) // padding
	replay.Skip(int(numberOfFormats * 8))            // formats

	var (
		root       uint32
		rootVisual uint32
	)
	replay.ReadUint32(&root)
	replay.Skip(4) // defaultColormap
	replay.Skip(4) // whitePixel
	replay.Skip(4) // blackPixel
	replay.Skip(4) // currentInputMask
	replay.Skip(2) // widthInPixels
	replay.Skip(2) // heightInPixels
	replay.Skip(2) // widthInMillimeters
	replay.Skip(2) // heightInMillimeters
	replay.Skip(2) // minInstalledMaps
	replay.Skip(2) // maxInstalledMaps
	replay.ReadUint32(&rootVisual)
	replay.Skip(1) // backingStores
	replay.Skip(1) // saveUnders
	replay.Skip(1) // rootDepth
	replay.Skip(1) // allowedDepthsLen

	// depth details follow, skip...

	var resourceID uint32
	return func() uint32 {
		id := (resourceID & resourceIDMask) | resourceIDBase
		resourceID++
		return id
	}, root, rootVisual, nil
}

// func internAtom(conn net.Conn) (uint32, uint32, error) {
// 	var b x11byte.Builder

// 	// Intern Atom
// 	b.AddUint8(X11_REQUEST_INTERN_ATOM) // opcode
// 	b.AddUint8(0)                       // onlyIfExists
// 	b.AddUint16(5)                      // requestLength
// 	b.AddUint16(12)                     // nameLength
// 	b.AddUint16(0)                      // unused
// 	b.AddBytes([]byte("WM_PROTOCOLS"))  // name

// 	// Intern Atom
// 	b.AddUint8(X11_REQUEST_INTERN_ATOM)    // opcode
// 	b.AddUint8(0)                          // onlyIfExists
// 	b.AddUint16(6)                         // requestLength
// 	b.AddUint16(16)                        // nameLength
// 	b.AddUint16(0)                         // unused
// 	b.AddBytes([]byte("WM_DELETE_WINDOW")) // name

// 	if _, err := conn.Write(b.BytesOrPanic()); err != nil {
// 		return 0, 0, err
// 	}

// 	var atoms [2]uint32
// 	for i := range 2 {
// 		header := x11byte.String(make([]byte, 32))
// 		if _, err := io.ReadFull(conn, header); err != nil {
// 			return 0, 0, err
// 		}

// 		var (
// 			reply          uint8
// 			sequenceNumber uint16
// 			replayLength   uint32
// 		)

// 		header.ReadUint8(&reply)
// 		header.Skip(1)
// 		header.ReadUint16(&sequenceNumber)
// 		header.ReadUint32(&replayLength)
// 		header.ReadUint32(&atoms[i])
// 		header.Skip(20)
// 	}

// 	return atoms[0], atoms[1], nil
// }

// func presentExtension(conn net.Conn) (uint8, error) {
// 	var b x11byte.Builder

// 	b.AddUint8(X11_REQUEST_QUERY_EXTENSION) // opcode
// 	b.AddUint8(1)                           // extension-minor, attach
// 	b.AddUint16(4)                          // requestLength
// 	b.AddUint16(7)                          // nameLength
// 	b.AddUint16(0)                          // unused
// 	b.AddBytes([]byte("Present"))           // name
// 	b.AddUint8(1)                           // unused

// 	if _, err := conn.Write(b.BytesOrPanic()); err != nil {
// 		return 0, err
// 	}

// 	header := x11byte.String(make([]byte, 32))
// 	if _, err := io.ReadFull(conn, header); err != nil {
// 		return 0, err
// 	}

// 	var (
// 		reply          uint8
// 		sequenceNumber uint16
// 		replayLength   uint32
// 		present        uint8
// 		majorOpcode    uint8
// 	)

// 	header.ReadUint8(&reply)
// 	header.Skip(1)
// 	header.ReadUint16(&sequenceNumber)
// 	header.ReadUint32(&replayLength)
// 	header.ReadUint8(&present)
// 	header.ReadUint8(&majorOpcode)
// 	header.Skip(1)
// 	header.Skip(1)

// 	return majorOpcode, nil
// }

// func dri3Extension(conn net.Conn) (uint8, error) {
// 	var b x11byte.Builder

// 	b.AddUint8(X11_REQUEST_QUERY_EXTENSION) // opcode
// 	b.AddUint8(1)                           // extension-minor, attach
// 	b.AddUint16(3)                          // requestLength
// 	b.AddUint16(4)                          // nameLength
// 	b.AddUint16(0)                          // unused
// 	b.AddBytes([]byte("DRI3"))              // name
// 	// b.AddUint8(4)                           // unused

// 	if _, err := conn.Write(b.BytesOrPanic()); err != nil {
// 		return 0, err
// 	}

// 	header := x11byte.String(make([]byte, 32))
// 	if _, err := io.ReadFull(conn, header); err != nil {
// 		return 0, err
// 	}

// 	var (
// 		reply          uint8
// 		sequenceNumber uint16
// 		replayLength   uint32
// 		present        uint8
// 		majorOpcode    uint8
// 	)

// 	header.ReadUint8(&reply)
// 	header.Skip(1)
// 	header.ReadUint16(&sequenceNumber)
// 	header.ReadUint32(&replayLength)
// 	header.ReadUint8(&present)
// 	header.ReadUint8(&majorOpcode)
// 	header.Skip(1)
// 	header.Skip(1)

// 	return majorOpcode, nil
// }

func pixmap(conn net.Conn, pixmapID, parentID uint32, dri3Opcode uint8) (int, error) {
	var b x11byte.Builder

	b.AddUint8(X11_REQUEST_CREATE_PIXMAP) // opcode
	b.AddUint8(24)                        // depth
	b.AddUint16(4)                        // requestLength
	b.AddUint32(pixmapID)                 // pid
	b.AddUint32(parentID)                 // drawable
	b.AddUint16(width)                    // width
	b.AddUint16(height)                   // height

	// // DRI3 Open
	// b.AddUint8(dri3Opcode) // opcode
	// b.AddUint8(1)          // extension-minor
	// b.AddUint16(3)         // requestLength
	// b.AddUint32(parentID)  // drawable
	// b.AddUint32(0)         // provider

	// DRI3 BufferFromPixmap
	b.AddUint8(dri3Opcode) // opcode
	b.AddUint8(3)          // extension-minor
	b.AddUint16(2)         // requestLength
	b.AddUint32(pixmapID)  // pixmap

	if _, err := conn.Write(b.BytesOrPanic()); err != nil {
		return 0, err
	}

	var fd uint32
	reply := x11byte.String(make([]byte, 1024))

	num := 4

	viaf, err := conn.(*net.UnixConn).File()
	if err != nil {
		return 0, err
	}
	socket := int(viaf.Fd())
	defer viaf.Close()

	control := make([]byte, syscall.CmsgSpace(num*4))
	_, _, _, _, err = syscall.Recvmsg(socket, reply, control, 0)
	if err != nil {
		return 0, err
	}

	// parse control msgs
	var msgs []syscall.SocketControlMessage
	msgs, err = syscall.ParseSocketControlMessage(control)
	if err != nil {
		return 0, err
	}

	fds := x11byte.String(msgs[0].Data)
	fds.ReadUint32(&fd)

	var (
		typ            uint8 // 1: reply
		nfd            uint8
		sequenceNumber uint16
		replayLength   uint32
	)

	reply.ReadUint8(&typ)
	reply.ReadUint8(&nfd)
	reply.ReadUint16(&sequenceNumber)
	reply.ReadUint32(&replayLength)

	println("reply", typ, nfd, sequenceNumber, replayLength)

	var (
		size   uint32
		width  uint16
		height uint16
		stride uint16
		depth  uint8
		bpp    uint8
		// fd     [3]uint32
	)

	reply.ReadUint32(&size)
	reply.ReadUint16(&width)
	reply.ReadUint16(&height)
	reply.ReadUint16(&stride)
	reply.ReadUint8(&depth)
	reply.ReadUint8(&bpp)

	println("reply2", size, width, height, depth, bpp, stride)

	println("done", fd)
	return int(fd), nil
}

const (
	DMA_BUF_IOCTL_SYNC = 0x40086200

	DMA_BUF_SYNC_READ             = 0x1 // Indicates that the mapped DMA buffer will be read by the client via the CPU map.
	DMA_BUF_SYNC_WRITE            = 0x2 // Indicates that the mapped DMA buffer will be written by the client via the CPU map.
	DMA_BUF_SYNC_RW               = 0x3 // An alias for DMA_BUF_SYNC_READ | DMA_BUF_SYNC_WRITE.
	DMA_BUF_SYNC_START            = 0x0 // Indicates the start of a map access session.
	DMA_BUF_SYNC_END              = 0x4 // Indicates the end of a map access session.
	DMA_BUF_SYNC_VALID_FLAGS_MASK = 0x7
	DMA_BUF_NAME_LEN              = 0x20
)

type dma_buf_sync struct {
	flags uint64
}
