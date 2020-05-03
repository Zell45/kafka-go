package protocol

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"reflect"
)

type encoder struct {
	writer io.Writer
	buffer [16]byte
	table  *crc32.Table
	crc32  uint32
}

func (e *encoder) Write(b []byte) (int, error) {
	n, err := e.writer.Write(b)
	if n > 0 && e.table != nil {
		e.crc32 = crc32.Update(e.crc32, e.table, b[:n])
	}
	return n, err
}

func (e *encoder) WriteString(s string) (int, error) {
	n, err := io.WriteString(e.writer, s)
	if n > 0 && e.table != nil {
		e.crc32 = crc32.Update(e.crc32, e.table, []byte(s[:n]))
	}
	return n, err
}

func (e *encoder) setCRC(table *crc32.Table) {
	e.table, e.crc32 = table, 0
}

func (e *encoder) encodeBool(v value) {
	b := int8(0)
	if v.bool() {
		b = 1
	}
	e.writeInt8(b)
}

func (e *encoder) encodeInt8(v value) {
	e.writeInt8(v.int8())
}

func (e *encoder) encodeInt16(v value) {
	e.writeInt16(v.int16())
}

func (e *encoder) encodeInt32(v value) {
	e.writeInt32(v.int32())
}

func (e *encoder) encodeInt64(v value) {
	e.writeInt64(v.int64())
}

func (e *encoder) encodeString(v value) {
	e.writeString(v.string())
}

func (e *encoder) encodeNullString(v value) {
	e.writeNullString(v.string())
}

func (e *encoder) encodeBytes(v value) {
	e.writeBytes(v.bytes())
}

func (e *encoder) encodeNullBytes(v value) {
	e.writeNullBytes(v.bytes())
}

func (e *encoder) encodeArray(v value, elemType reflect.Type, encodeElem encodeFunc) {
	a := v.array(elemType)
	n := a.length()
	e.writeInt32(int32(n))

	for i := 0; i < n; i++ {
		encodeElem(e, a.index(i))
	}
}

func (e *encoder) encodeNullArray(v value, elemType reflect.Type, encodeElem encodeFunc) {
	a := v.array(elemType)
	if a.isNil() {
		e.writeInt32(-1)
		return
	}

	n := a.length()
	e.writeInt32(int32(n))

	for i := 0; i < n; i++ {
		encodeElem(e, a.index(i))
	}
}

func (e *encoder) writeInt8(i int8) {
	writeInt8(e.buffer[:1], i)
	e.Write(e.buffer[:1])
}

func (e *encoder) writeInt16(i int16) {
	writeInt16(e.buffer[:2], i)
	e.Write(e.buffer[:2])
}

func (e *encoder) writeInt32(i int32) {
	writeInt32(e.buffer[:4], i)
	e.Write(e.buffer[:4])
}

func (e *encoder) writeInt64(i int64) {
	writeInt64(e.buffer[:8], i)
	e.Write(e.buffer[:8])
}

func (e *encoder) writeString(s string) {
	e.writeInt16(int16(len(s)))
	e.WriteString(s)
}

func (e *encoder) writeNullString(s string) {
	if s == "" {
		e.writeInt16(-1)
	} else {
		e.writeInt16(int16(len(s)))
		e.WriteString(s)
	}
}

func (e *encoder) writeCompactString(s string) {
	e.writeVarInt(int64(len(s)))
	e.WriteString(s)
}

func (e *encoder) writeCompactNullString(s string) {
	if s == "" {
		e.writeVarInt(-1)
	} else {
		e.writeVarInt(int64(len(s)))
		e.WriteString(s)
	}
}

func (e *encoder) writeBytes(b []byte) {
	e.writeInt32(int32(len(b)))
	e.Write(b)
}

func (e *encoder) writeNullBytes(b []byte) {
	if b == nil {
		e.writeInt32(-1)
	} else {
		e.writeInt32(int32(len(b)))
		e.Write(b)
	}
}

func (e *encoder) writeCompactBytes(b []byte) {
	e.writeVarInt(int64(len(b)))
	e.Write(b)
}

func (e *encoder) writeCompactNullBytes(b []byte) {
	if b == nil {
		e.writeVarInt(-1)
	} else {
		e.writeVarInt(int64(len(b)))
		e.Write(b)
	}
}

func (e *encoder) writeBytesFrom(b ByteSequence) error {
	e.writeInt32(int32(b.Size()))
	_, err := copyBytes(e, b)
	return err
}

func (e *encoder) writeNullBytesFrom(b ByteSequence) error {
	if b == nil {
		e.writeInt32(-1)
		return nil
	} else {
		e.writeInt32(int32(b.Size()))
		_, err := copyBytes(e, b)
		return err
	}
}

func (e *encoder) writeCompactNullBytesFrom(b ByteSequence) error {
	if b == nil {
		e.writeVarInt(-1)
		return nil
	} else {
		e.writeVarInt(b.Size())
		_, err := copyBytes(e, b)
		return err
	}
}

func (e *encoder) writeVarInt(i int64) {
	b := e.buffer[:]
	u := uint64((i << 1) ^ (i >> 63))
	n := 0

	for u >= 0x80 && n < len(b) {
		b[n] = byte(u) | 0x80
		u >>= 7
		n++
	}

	if n < len(b) {
		b[n] = byte(u)
		n++
	}

	e.Write(b[:n])
}

type encodeFunc func(*encoder, value)

var (
	writerTo = reflect.TypeOf((*io.WriterTo)(nil)).Elem()
)

func encodeFuncOf(typ reflect.Type, version int16, tag structTag) encodeFunc {
	if reflect.PtrTo(typ).Implements(writerTo) {
		return writerEncodeFuncOf(typ)
	}
	switch typ.Kind() {
	case reflect.Bool:
		return (*encoder).encodeBool
	case reflect.Int8:
		return (*encoder).encodeInt8
	case reflect.Int16:
		return (*encoder).encodeInt16
	case reflect.Int32:
		return (*encoder).encodeInt32
	case reflect.Int64:
		return (*encoder).encodeInt64
	case reflect.String:
		return stringEncodeFuncOf(tag)
	case reflect.Struct:
		return structEncodeFuncOf(typ, version)
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 { // []byte
			return bytesEncodeFuncOf(tag)
		}
		return arrayEncodeFuncOf(typ, version, tag)
	default:
		panic("unsupported type: " + typ.String())
	}
}

func stringEncodeFuncOf(tag structTag) encodeFunc {
	switch {
	case tag.Nullable:
		return (*encoder).encodeNullString
	default:
		return (*encoder).encodeString
	}
}

func bytesEncodeFuncOf(tag structTag) encodeFunc {
	switch {
	case tag.Nullable:
		return (*encoder).encodeNullBytes
	default:
		return (*encoder).encodeBytes
	}
}

func structEncodeFuncOf(typ reflect.Type, version int16) encodeFunc {
	type field struct {
		encode encodeFunc
		index  index
	}

	var fields []field
	forEachStructField(typ, func(typ reflect.Type, index index, tag string) {
		if typ.Size() != 0 { // skip struct{}
			forEachStructTag(tag, func(tag structTag) bool {
				if tag.MinVersion <= version && version <= tag.MaxVersion {
					fields = append(fields, field{
						encode: encodeFuncOf(typ, version, tag),
						index:  index,
					})
					return false
				}
				return true
			})
		}
	})

	return func(e *encoder, v value) {
		for i := range fields {
			f := &fields[i]
			f.encode(e, v.fieldByIndex(f.index))
		}
	}
}

func arrayEncodeFuncOf(typ reflect.Type, version int16, tag structTag) encodeFunc {
	elemType := typ.Elem()
	elemFunc := encodeFuncOf(elemType, version, tag)
	switch {
	case tag.Nullable:
		return func(e *encoder, v value) { e.encodeNullArray(v, elemType, elemFunc) }
	default:
		return func(e *encoder, v value) { e.encodeArray(v, elemType, elemFunc) }
	}
}

func writerEncodeFuncOf(typ reflect.Type) encodeFunc {
	typ = reflect.PtrTo(typ)
	return func(e *encoder, v value) { v.iface(typ).(io.WriterTo).WriteTo(e) }
}

func writeInt8(b []byte, i int8) {
	b[0] = byte(i)
}

func writeInt16(b []byte, i int16) {
	binary.BigEndian.PutUint16(b, uint16(i))
}

func writeInt32(b []byte, i int32) {
	binary.BigEndian.PutUint32(b, uint32(i))
}

func writeInt64(b []byte, i int64) {
	binary.BigEndian.PutUint64(b, uint64(i))
}