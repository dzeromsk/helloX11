// This is a simple X11 application that displays image and supports WM_DELETE_WINDOW
package main

import (
	"encoding/hex"
	"errors"
	"io"
	"net"

	"github.com/dzeromsk/helloX11/x11byte"

	"github.com/gen2brain/shm"
)

const (
	width  = 1024
	height = 1024
)

func main() {
	// Prepare Image
	shmid, err := shm.Get(shm.IPC_PRIVATE, width*height*4, shm.IPC_CREAT|0600)
	if err != nil {
		panic(err)
	}

	shmaddr, err := shm.At(shmid, 0, 0)
	if err != nil {
		panic(err)
	}
	defer shm.Dt(shmaddr)
	defer shm.Ctl(shmid, shm.IPC_RMID, nil)

	for i := range height {
		for j := range width {
			offset := (i*width + j) * 4
			shmaddr[offset+0] = uint8(i / 4 % 256)
			shmaddr[offset+1] = uint8(j / 4 % 256)
			shmaddr[offset+2] = 0x00
			shmaddr[offset+3] = 0x00
		}
	}

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

	// TODO(dzeromsk): verify mitshmOpcode is static
	// mitshmOpcode, err := mitshmExtension(conn)
	// if err != nil {
	// 	panic(err)
	// }
	mitshmOpcode := uint8(130)
	// println(mitshmOpcode)

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

	shmID := nextID()

	// MIT-SHM Attach, to avoid image data copy
	b.AddUint8(mitshmOpcode)   // opcode
	b.AddUint8(1)              // extension-minor, attach
	b.AddUint16(4)             // requestLength
	b.AddUint32(shmID)         // shmseg
	b.AddUint32(uint32(shmid)) // shmid
	b.AddUint8(0)              // read only
	b.AddUint24(0)             // unused

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

	gcID := nextID()

	// Create GC, needed by mit-shm PutImage
	b.AddUint8(X11_REQUEST_CREATE_GC)   // opcode
	b.AddUint8(0)                       // unused
	b.AddUint16(5)                      // requestLength
	b.AddUint32(gcID)                   // cid
	b.AddUint32(windowID)               // drawable
	b.AddUint32(X11_GC_FLAG_BACKGROUND) // gcValueMask
	b.AddUint32(0x00000000)             // background

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
				dstx, dsty := max((int(w)-width)/2, 0), max((int(h)-height)/2, 0)
				// MIT-SHM PutImage
				r.AddUint8(mitshmOpcode) // opcode
				r.AddUint8(3)            // extension-minor, PutImage
				r.AddUint16(10)          // requestLength
				r.AddUint32(windowID)    // drawable
				r.AddUint32(gcID)        // gc
				r.AddUint16(width)       // totalWidth
				r.AddUint16(height)      // totalHeight
				r.AddUint16(0)           // srcX
				r.AddUint16(0)           // srcY
				r.AddUint16(width)       // srcWidth
				r.AddUint16(height)      // srcHeight
				// r.AddUint16(0)           // dstX
				// r.AddUint16(0)           // dstY
				r.AddUint16(uint16(dstx)) // dstX
				r.AddUint16(uint16(dsty)) // dstY
				r.AddUint8(24)            // depth
				r.AddUint8(2)             // format
				r.AddUint8(0)             // sendEvent
				r.AddUint8(0)             // unused
				r.AddUint32(shmID)        // shmseg
				r.AddUint32(0)            // offset

				put := r.BytesOrPanic()

				if _, err := conn.Write(put); err != nil {
					panic(err)
				}
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

// func mitshmExtension(conn net.Conn) (uint8, error) {
// 	var b x11byte.Builder

// 	b.AddUint8(X11_REQUEST_QUERY_EXTENSION) // opcode
// 	b.AddUint8(1)                           // extension-minor, attach
// 	b.AddUint16(4)                          // requestLength
// 	b.AddUint16(7)                          // nameLength
// 	b.AddUint16(0)                          // unused
// 	b.AddBytes([]byte("MIT-SHM"))           // name
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
