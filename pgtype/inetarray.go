package pgtype

import (
	"bytes"
	"fmt"
	"io"
	"net"

	"github.com/jackc/pgx/pgio"
)

type InetArray struct {
	Elements   []Inet
	Dimensions []ArrayDimension
	Status     Status
}

func (dst *InetArray) ConvertFrom(src interface{}) error {
	switch value := src.(type) {
	case InetArray:
		*dst = value
	case CidrArray:
		*dst = InetArray(value)
	case []*net.IPNet:
		if value == nil {
			*dst = InetArray{Status: Null}
		} else if len(value) == 0 {
			*dst = InetArray{Status: Present}
		} else {
			elements := make([]Inet, len(value))
			for i := range value {
				if err := elements[i].ConvertFrom(value[i]); err != nil {
					return err
				}
			}
			*dst = InetArray{
				Elements:   elements,
				Dimensions: []ArrayDimension{{Length: int32(len(elements)), LowerBound: 1}},
				Status:     Present,
			}
		}
	case []net.IP:
		if value == nil {
			*dst = InetArray{Status: Null}
		} else if len(value) == 0 {
			*dst = InetArray{Status: Present}
		} else {
			elements := make([]Inet, len(value))
			for i := range value {
				if err := elements[i].ConvertFrom(value[i]); err != nil {
					return err
				}
			}
			*dst = InetArray{
				Elements:   elements,
				Dimensions: []ArrayDimension{{Length: int32(len(elements)), LowerBound: 1}},
				Status:     Present,
			}
		}
	default:
		if originalSrc, ok := underlyingSliceType(src); ok {
			return dst.ConvertFrom(originalSrc)
		}
		return fmt.Errorf("cannot convert %v to Inet", value)
	}

	return nil
}

func (src *InetArray) AssignTo(dst interface{}) error {
	switch v := dst.(type) {

	case *[]*net.IPNet:
		if src.Status == Present {
			*v = make([]*net.IPNet, len(src.Elements))
			for i := range src.Elements {
				if err := src.Elements[i].AssignTo(&((*v)[i])); err != nil {
					return err
				}
			}
		} else {
			*v = nil
		}
	case *[]net.IP:
		if src.Status == Present {
			*v = make([]net.IP, len(src.Elements))
			for i := range src.Elements {
				if err := src.Elements[i].AssignTo(&((*v)[i])); err != nil {
					return err
				}
			}
		} else {
			*v = nil
		}

	default:
		if originalDst, ok := underlyingPtrSliceType(dst); ok {
			return src.AssignTo(originalDst)
		}
		return fmt.Errorf("cannot put decode %v into %T", src, dst)
	}

	return nil
}

func (dst *InetArray) DecodeText(r io.Reader) error {
	size, err := pgio.ReadInt32(r)
	if err != nil {
		return err
	}

	if size == -1 {
		*dst = InetArray{Status: Null}
		return nil
	}

	buf := make([]byte, int(size))
	_, err = io.ReadFull(r, buf)
	if err != nil {
		return err
	}

	uta, err := ParseUntypedTextArray(string(buf))
	if err != nil {
		return err
	}

	textElementReader := NewTextElementReader(r)
	var elements []Inet

	if len(uta.Elements) > 0 {
		elements = make([]Inet, len(uta.Elements))

		for i, s := range uta.Elements {
			var elem Inet
			textElementReader.Reset(s)
			err = elem.DecodeText(textElementReader)
			if err != nil {
				return err
			}

			elements[i] = elem
		}
	}

	*dst = InetArray{Elements: elements, Dimensions: uta.Dimensions, Status: Present}

	return nil
}

func (dst *InetArray) DecodeBinary(r io.Reader) error {
	size, err := pgio.ReadInt32(r)
	if err != nil {
		return err
	}

	if size == -1 {
		*dst = InetArray{Status: Null}
		return nil
	}

	var arrayHeader ArrayHeader
	err = arrayHeader.DecodeBinary(r)
	if err != nil {
		return err
	}

	if len(arrayHeader.Dimensions) == 0 {
		*dst = InetArray{Dimensions: arrayHeader.Dimensions, Status: Present}
		return nil
	}

	elementCount := arrayHeader.Dimensions[0].Length
	for _, d := range arrayHeader.Dimensions[1:] {
		elementCount *= d.Length
	}

	elements := make([]Inet, elementCount)

	for i := range elements {
		err = elements[i].DecodeBinary(r)
		if err != nil {
			return err
		}
	}

	*dst = InetArray{Elements: elements, Dimensions: arrayHeader.Dimensions, Status: Present}
	return nil
}

func (src *InetArray) EncodeText(w io.Writer) error {
	if done, err := encodeNotPresent(w, src.Status); done {
		return err
	}

	if len(src.Dimensions) == 0 {
		_, err := pgio.WriteInt32(w, 2)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte("{}"))
		return err
	}

	buf := &bytes.Buffer{}

	err := EncodeTextArrayDimensions(buf, src.Dimensions)
	if err != nil {
		return err
	}

	// dimElemCounts is the multiples of elements that each array lies on. For
	// example, a single dimension array of length 4 would have a dimElemCounts of
	// [4]. A multi-dimensional array of lengths [3,5,2] would have a
	// dimElemCounts of [30,10,2]. This is used to simplify when to render a '{'
	// or '}'.
	dimElemCounts := make([]int, len(src.Dimensions))
	dimElemCounts[len(src.Dimensions)-1] = int(src.Dimensions[len(src.Dimensions)-1].Length)
	for i := len(src.Dimensions) - 2; i > -1; i-- {
		dimElemCounts[i] = int(src.Dimensions[i].Length) * dimElemCounts[i+1]
	}

	textElementWriter := NewTextElementWriter(buf)

	for i, elem := range src.Elements {
		if i > 0 {
			err = pgio.WriteByte(buf, ',')
			if err != nil {
				return err
			}
		}

		for _, dec := range dimElemCounts {
			if i%dec == 0 {
				err = pgio.WriteByte(buf, '{')
				if err != nil {
					return err
				}
			}
		}

		textElementWriter.Reset()
		err = elem.EncodeText(textElementWriter)
		if err != nil {
			return err
		}

		for _, dec := range dimElemCounts {
			if (i+1)%dec == 0 {
				err = pgio.WriteByte(buf, '}')
				if err != nil {
					return err
				}
			}
		}
	}

	_, err = pgio.WriteInt32(w, int32(buf.Len()))
	if err != nil {
		return err
	}

	_, err = buf.WriteTo(w)
	return err
}

func (src *InetArray) EncodeBinary(w io.Writer) error {
	return src.encodeBinary(w, InetOID)
}

func (src *InetArray) encodeBinary(w io.Writer, elementOID int32) error {
	if done, err := encodeNotPresent(w, src.Status); done {
		return err
	}

	var arrayHeader ArrayHeader

	// TODO - consider how to avoid having to buffer array before writing length -
	// or how not pay allocations for the byte order conversions.
	elemBuf := &bytes.Buffer{}

	for i := range src.Elements {
		err := src.Elements[i].EncodeBinary(elemBuf)
		if err != nil {
			return err
		}
		if src.Elements[i].Status == Null {
			arrayHeader.ContainsNull = true
		}
	}

	arrayHeader.ElementOID = elementOID
	arrayHeader.Dimensions = src.Dimensions

	// TODO - consider how to avoid having to buffer array before writing length -
	// or how not pay allocations for the byte order conversions.
	headerBuf := &bytes.Buffer{}
	err := arrayHeader.EncodeBinary(headerBuf)
	if err != nil {
		return err
	}

	_, err = pgio.WriteInt32(w, int32(headerBuf.Len()+elemBuf.Len()))
	if err != nil {
		return err
	}

	_, err = headerBuf.WriteTo(w)
	if err != nil {
		return err
	}

	_, err = elemBuf.WriteTo(w)
	if err != nil {
		return err
	}

	return err
}
