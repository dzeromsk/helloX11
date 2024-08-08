# helloX11

X11 golang app without C libraries (X11, XCB). In this example we are drawing a bitmap from memory.

## Features
* Direct communication with X11 server over UNIX or TCP socket
* Uses `MIT-SHM` `Attach` and `PutImage` for fast(er) bitmap transfer
* Supports `WM_DELETE_WINDOW` event setup and handles exit request gracefully

## Related work
* https://hereket.com/posts/from-scratch-x11-windowing/