// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package x11byte

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
)

func builderBytesEq(b *Builder, want ...byte) error {
	got := b.BytesOrPanic()
	if !bytes.Equal(got, want) {
		return fmt.Errorf("Bytes() = %v, want %v", got, want)
	}
	return nil
}

func TestContinuationError(t *testing.T) {
	const errorStr = "TestContinuationError"
	var b Builder
	b.AddUint8LengthPrefixed(func(b *Builder) {
		b.AddUint8(1)
		panic(BuildError{Err: errors.New(errorStr)})
	})

	ret, err := b.Bytes()
	if ret != nil {
		t.Error("expected nil result")
	}
	if err == nil {
		t.Fatal("unexpected nil error")
	}
	if s := err.Error(); s != errorStr {
		t.Errorf("expected error %q, got %v", errorStr, s)
	}
}

func TestContinuationNonError(t *testing.T) {
	defer func() {
		recover()
	}()

	var b Builder
	b.AddUint8LengthPrefixed(func(b *Builder) {
		b.AddUint8(1)
		panic(1)
	})

	t.Error("Builder did not panic")
}

func TestGeneratedPanic(t *testing.T) {
	defer func() {
		recover()
	}()

	var b Builder
	b.AddUint8LengthPrefixed(func(b *Builder) {
		var p *byte
		*p = 0
	})

	t.Error("Builder did not panic")
}

func TestBytes(t *testing.T) {
	var b Builder
	v := []byte("foobarbaz")
	b.AddBytes(v[0:3])
	b.AddBytes(v[3:4])
	b.AddBytes(v[4:9])
	if err := builderBytesEq(&b, v...); err != nil {
		t.Error(err)
	}
	s := String(b.BytesOrPanic())
	for _, w := range []string{"foo", "bar", "baz"} {
		var got []byte
		if !s.ReadBytes(&got, 3) {
			t.Errorf("ReadBytes() = false, want true (w = %v)", w)
		}
		want := []byte(w)
		if !bytes.Equal(got, want) {
			t.Errorf("ReadBytes(): got = %v, want %v", got, want)
		}
	}
	if len(s) != 0 {
		t.Errorf("len(s) = %d, want 0", len(s))
	}
}

func TestUint8(t *testing.T) {
	var b Builder
	b.AddUint8(42)
	if err := builderBytesEq(&b, 42); err != nil {
		t.Error(err)
	}

	var s String = b.BytesOrPanic()
	var v uint8
	if !s.ReadUint8(&v) {
		t.Error("ReadUint8() = false, want true")
	}
	if v != 42 {
		t.Errorf("v = %d, want 42", v)
	}
	if len(s) != 0 {
		t.Errorf("len(s) = %d, want 0", len(s))
	}
}

func TestUint16(t *testing.T) {
	var b Builder
	b.AddUint16(65534)
	if err := builderBytesEq(&b, 254, 255); err != nil {
		t.Error(err)
	}
	var s String = b.BytesOrPanic()
	var v uint16
	if !s.ReadUint16(&v) {
		t.Error("ReadUint16() == false, want true")
	}
	if v != 65534 {
		t.Errorf("v = %d, want 65534", v)
	}
	if len(s) != 0 {
		t.Errorf("len(s) = %d, want 0", len(s))
	}
}

func TestUint24(t *testing.T) {
	var b Builder
	b.AddUint24(0xfffefd)
	if err := builderBytesEq(&b, 253, 254, 255); err != nil {
		t.Error(err)
	}

	var s String = b.BytesOrPanic()
	var v uint32
	if !s.ReadUint24(&v) {
		t.Error("ReadUint24() = false, want true")
	}
	if v != 0xfffefd {
		t.Errorf("v = %d, want fffefd", v)
	}
	if len(s) != 0 {
		t.Errorf("len(s) = %d, want 0", len(s))
	}
}

func TestUint24Truncation(t *testing.T) {
	var b Builder
	b.AddUint24(0x10111213)
	if err := builderBytesEq(&b, 0x13, 0x12, 0x11); err != nil {
		t.Error(err)
	}
}

func TestUint32(t *testing.T) {
	var b Builder
	b.AddUint32(0xfffefdfc)
	if err := builderBytesEq(&b, 252, 253, 254, 255); err != nil {
		t.Error(err)
	}

	var s String = b.BytesOrPanic()
	var v uint32
	if !s.ReadUint32(&v) {
		t.Error("ReadUint32() = false, want true")
	}
	if v != 0xfffefdfc {
		t.Errorf("v = %x, want fffefdfc", v)
	}
	if len(s) != 0 {
		t.Errorf("len(s) = %d, want 0", len(s))
	}
}

func TestUMultiple(t *testing.T) {
	var b Builder
	b.AddUint8(23)
	b.AddUint32(0xfffefdfc)
	b.AddUint16(42)
	if err := builderBytesEq(&b, 23, 252, 253, 254, 255, 42, 0); err != nil {
		t.Error(err)
	}

	var s String = b.BytesOrPanic()
	var (
		x uint8
		y uint32
		z uint16
	)
	if !s.ReadUint8(&x) || !s.ReadUint32(&y) || !s.ReadUint16(&z) {
		t.Error("ReadUint8() = false, want true")
	}
	if x != 23 || y != 0xfffefdfc || z != 42 {
		t.Errorf("x, y, z = %d, %d, %d; want 23, 4294901244, 5", x, y, z)
	}
	if len(s) != 0 {
		t.Errorf("len(s) = %d, want 0", len(s))
	}
}

func TestUint8LengthPrefixedSimple(t *testing.T) {
	var b Builder
	b.AddUint8LengthPrefixed(func(c *Builder) {
		c.AddUint8(23)
		c.AddUint8(42)
	})
	if err := builderBytesEq(&b, 3, 23, 42); err != nil {
		t.Error(err)
	}

	var base, child String = b.BytesOrPanic(), nil
	var x, y uint8
	if !base.ReadUint8LengthPrefixed(&child) || !child.ReadUint8(&x) ||
		!child.ReadUint8(&y) {
		t.Error("parsing failed")
	}
	if x != 23 || y != 42 {
		t.Errorf("want x, y == 23, 42; got %d, %d", x, y)
	}
	if len(base) != 0 {
		t.Errorf("len(base) = %d, want 0", len(base))
	}
	if len(child) != 0 {
		t.Errorf("len(child) = %d, want 0", len(child))
	}
}

func TestUint8LengthPrefixedMulti(t *testing.T) {
	var b Builder
	b.AddUint8LengthPrefixed(func(c *Builder) {
		c.AddUint8(23)
		c.AddUint8(42)
	})
	b.AddUint8(5)
	b.AddUint8LengthPrefixed(func(c *Builder) {
		c.AddUint8(123)
		c.AddUint8(234)
	})
	if err := builderBytesEq(&b, 3, 23, 42, 5, 3, 123, 234); err != nil {
		t.Error(err)
	}

	var s, child String = b.BytesOrPanic(), nil
	var u, v, w, x, y uint8
	if !s.ReadUint8LengthPrefixed(&child) || !child.ReadUint8(&u) || !child.ReadUint8(&v) ||
		!s.ReadUint8(&w) || !s.ReadUint8LengthPrefixed(&child) || !child.ReadUint8(&x) || !child.ReadUint8(&y) {
		t.Error("parsing failed")
	}
	if u != 23 || v != 42 || w != 5 || x != 123 || y != 234 {
		t.Errorf("u, v, w, x, y = %d, %d, %d, %d, %d; want 23, 42, 5, 123, 234",
			u, v, w, x, y)
	}
	if len(s) != 0 {
		t.Errorf("len(s) = %d, want 0", len(s))
	}
	if len(child) != 0 {
		t.Errorf("len(child) = %d, want 0", len(child))
	}
}

func TestUint8LengthPrefixedNested(t *testing.T) {
	var b Builder
	b.AddUint8LengthPrefixed(func(c *Builder) {
		c.AddUint8(5)
		c.AddUint8LengthPrefixed(func(d *Builder) {
			d.AddUint8(23)
			d.AddUint8(42)
		})
		c.AddUint8(123)
	})
	if err := builderBytesEq(&b, 6, 5, 3, 23, 42, 123); err != nil {
		t.Error(err)
	}

	var base, child1, child2 String = b.BytesOrPanic(), nil, nil
	var u, v, w, x uint8
	if !base.ReadUint8LengthPrefixed(&child1) {
		t.Error("parsing base failed")
	}
	if !child1.ReadUint8(&u) || !child1.ReadUint8LengthPrefixed(&child2) || !child1.ReadUint8(&x) {
		t.Error("parsing child1 failed")
	}
	if !child2.ReadUint8(&v) || !child2.ReadUint8(&w) {
		t.Error("parsing child2 failed")
	}
	if u != 5 || v != 23 || w != 42 || x != 123 {
		t.Errorf("u, v, w, x = %d, %d, %d, %d, want 5, 23, 42, 123",
			u, v, w, x)
	}
	if len(base) != 0 {
		t.Errorf("len(base) = %d, want 0", len(base))
	}
	if len(child1) != 0 {
		t.Errorf("len(child1) = %d, want 0", len(child1))
	}
	if len(base) != 0 {
		t.Errorf("len(child2) = %d, want 0", len(child2))
	}
}

func TestPreallocatedBuffer(t *testing.T) {
	var buf [5]byte
	b := NewBuilder(buf[0:0])
	b.AddUint8(1)
	b.AddUint8LengthPrefixed(func(c *Builder) {
		c.AddUint8(3)
		c.AddUint8(4)
	})
	b.AddUint16(1286) // Outgrow buf by one byte.
	want := []byte{1, 3, 3, 4, 0}
	if !bytes.Equal(buf[:], want) {
		t.Errorf("buf = %v want %v", buf, want)
	}
	if err := builderBytesEq(b, 1, 3, 3, 4, 6, 5); err != nil {
		t.Error(err)
	}
}

func TestWriteWithPendingChild(t *testing.T) {
	var b Builder
	b.AddUint8LengthPrefixed(func(c *Builder) {
		c.AddUint8LengthPrefixed(func(d *Builder) {
			func() {
				defer func() {
					if recover() == nil {
						t.Errorf("recover() = nil, want error; c.AddUint8() did not panic")
					}
				}()
				c.AddUint8(2) // panics
			}()

			defer func() {
				if recover() == nil {
					t.Errorf("recover() = nil, want error; b.AddUint8() did not panic")
				}
			}()
			b.AddUint8(2) // panics
		})

		defer func() {
			if recover() == nil {
				t.Errorf("recover() = nil, want error; b.AddUint8() did not panic")
			}
		}()
		b.AddUint8(2) // panics
	})
}

func TestSetError(t *testing.T) {
	const errorStr = "TestSetError"
	var b Builder
	b.SetError(errors.New(errorStr))

	ret, err := b.Bytes()
	if ret != nil {
		t.Error("expected nil result")
	}
	if err == nil {
		t.Fatal("unexpected nil error")
	}
	if s := err.Error(); s != errorStr {
		t.Errorf("expected error %q, got %v", errorStr, s)
	}
}

func TestUnwrite(t *testing.T) {
	var b Builder
	b.AddBytes([]byte{1, 2, 3, 4, 5})
	b.Unwrite(2)
	if err := builderBytesEq(&b, 1, 2, 3); err != nil {
		t.Error(err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("recover() = nil, want error; b.Unwrite() did not panic")
			}
		}()
		b.Unwrite(4) // panics
	}()

	b = Builder{}
	b.AddBytes([]byte{1, 2, 3, 4, 5})
	b.AddUint8LengthPrefixed(func(b *Builder) {
		b.AddBytes([]byte{1, 2, 3, 4, 5})

		defer func() {
			if recover() == nil {
				t.Errorf("recover() = nil, want error; b.Unwrite() did not panic")
			}
		}()
		b.Unwrite(6) // panics
	})

	b = Builder{}
	b.AddBytes([]byte{1, 2, 3, 4, 5})
	b.AddUint8LengthPrefixed(func(c *Builder) {
		defer func() {
			if recover() == nil {
				t.Errorf("recover() = nil, want error; b.Unwrite() did not panic")
			}
		}()
		b.Unwrite(2) // panics (attempted unwrite while child is pending)
	})
}

func TestFixedBuilderLengthPrefixed(t *testing.T) {
	bufCap := 10
	inner := bytes.Repeat([]byte{0xff}, bufCap-2)
	buf := make([]byte, 0, bufCap)
	b := NewFixedBuilder(buf)
	b.AddUint16LengthPrefixed(func(b *Builder) {
		b.AddBytes(inner)
	})
	if got := b.BytesOrPanic(); len(got) != bufCap {
		t.Errorf("Expected output length to be %d, got %d", bufCap, len(got))
	}
}

func TestFixedBuilderPanicReallocate(t *testing.T) {
	defer func() {
		recover()
	}()

	b := NewFixedBuilder(make([]byte, 0, 10))
	b1 := NewFixedBuilder(make([]byte, 0, 10))
	b.AddUint16LengthPrefixed(func(b *Builder) {
		*b = *b1
	})

	t.Error("Builder did not panic")
}
