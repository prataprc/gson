// Package also implements RFC-6901, JSON pointers.
// Pointers themself can be encoded into cbor format and
// vice-versa.
//
//   cbor-path :    | Text-chunk-start |
//                        | tagJsonString | len | segment1 |
//                        | tagJsonString | len | segment2 |
//                        ...
//                  | Break-stop |
package cbor

import "strconv"
import "bytes"

//import "fmt"
import "errors"

// ErrorInvalidArrayOffset
var ErrorInvalidArrayOffset = errors.New("cbor.invalidArrayOffset")

// ErrorInvalidPointer
var ErrorInvalidPointer = errors.New("cbor.invalidPointer")

// ErrorNoKey
var ErrorNoKey = errors.New("cbor.noKey")

// ErrorExpectedCborKey
var ErrorExpectedCborKey = errors.New("cbor.expectedCborKey")

// ErrorMalformedDocument
var ErrorMalformedDocument = errors.New("cbor.malformedDocument")

// ErrorInvalidDocument
var ErrorInvalidDocument = errors.New("cbor.invalidDocument")

// ErrorUnknownType to encode
var ErrorUnknownType = errors.New("cbor.unknownType")

const maxPartSize int = 1024

func fromJsonPointer(jsonptr, out []byte) int {
	var part [maxPartSize]byte

	n, off := encodeTextStart(out), 0
	for i := 0; i < len(jsonptr); {
		if jsonptr[i] == '~' {
			if jsonptr[i+1] == '1' {
				part[off] = '/'
				off, i = off+1, i+2

			} else if jsonptr[i+1] == '0' {
				part[off] = '~'
				off, i = off+1, i+2
			}

		} else if jsonptr[i] == '/' {
			if off > 0 {
				n += encodeTag(uint64(tagJsonString), out[n:])
				n += encodeText(bytes2str(part[:off]), out[n:])
				off = 0
			}
			i++

		} else {
			part[off] = jsonptr[i]
			i, off = i+1, off+1
		}
	}
	if off > 0 || (len(jsonptr) > 0 && jsonptr[len(jsonptr)-1] == '/') {
		n += encodeTag(uint64(tagJsonString), out[n:])
		n += encodeText(bytes2str(part[:off]), out[n:])
	}

	n += encodeBreakStop(out[n:])
	return n
}

func toJsonPointer(cborptr, out []byte) int {
	i, n, brkstp := 1, 0, hdr(type7, itemBreak)
	for {
		if cborptr[i] == hdr(type6, info24) && cborptr[i+1] == tagJsonString {
			i, out[n] = i+2, '/'
			n += 1
			ln, j := decodeLength(cborptr[i:])
			ln, i = ln+i+j, i+j
			for i < ln {
				switch cborptr[i] {
				case '/':
					out[n], out[n+1] = '~', '1'
					n += 2
				case '~':
					out[n], out[n+1] = '~', '0'
					n += 2
				default:
					out[n] = cborptr[i]
					n += 1
				}
				i++
			}
		}
		if cborptr[i] == brkstp {
			break
		}
	}
	return n
}

func partial(part, doc []byte) (start, end int) {
	if doc[0] == hdr(type4, byte(indefiniteLength)) { // array
		var index int
		var err error
		if part[0] == '-' {
			index = -1
		} else if index, err = strconv.Atoi(bytes2str(part)); err != nil {
			panic(ErrorInvalidArrayOffset)
		}
		n := 1
		n += arrayIndex(doc[1:], index)
		m := itemsEnd(doc[n:])
		//fmt.Println("partial-arr", index, n, n+m, doc[n:n+m], string(part))
		return n, n + m

	} else if doc[0] == hdr(type5, byte(indefiniteLength)) { // map
		n := 1
		n += mapIndex(doc[n:], part)
		m := itemsEnd(doc[n:])   // key
		p := itemsEnd(doc[n+m:]) // value
		//fmt.Println("partial", n, n+m, n+m+p, doc[n+m:n+m+p], string(part))
		return n + m, n + m + p
	}
	panic(ErrorInvalidPointer)
}

func lookup(cborptr, doc []byte) (start, end int) {
	i, brkstp := 1, hdr(type7, itemBreak)
	n, m := 0, len(doc)
	start, end = n, m
	if cborptr[i] == brkstp { // cborptr is empty ""
		return start, end
	}
	for i < len(cborptr) && cborptr[i] != brkstp {
		doc = doc[n:m]
		if cborptr[i] == hdr(type6, info24) && cborptr[i+1] == tagJsonString {
			i += 2
			ln, j := decodeLength(cborptr[i:])
			n, m = partial(cborptr[i+j:i+j+ln], doc)
			i += j + ln
			start += n
			end = start + (m - n)
			//fmt.Println("len", ln, i, j, n, m, start, end, len(cborptr))
			continue
		}
		panic(ErrorInvalidPointer)
	}
	return start, end
}

func arrayIndex(arr []byte, index int) int {
	count, prev, n, brkstp := 0, 0, 0, hdr(type7, itemBreak)
	for arr[n] != brkstp {
		if count == index {
			return n
		} else if arr[n] == brkstp {
			panic(ErrorInvalidArrayOffset)
		}
		prev = n
		n += itemsEnd(arr[n:])
		count++
	}
	if index == -1 && arr[n] == brkstp {
		return prev
	}
	panic(ErrorInvalidArrayOffset)
}

func mapIndex(buf []byte, part []byte) int {
	n, brkstp := 0, hdr(type7, itemBreak)
	for n < len(buf) {
		start := n
		if buf[n] == brkstp {
			panic(ErrorNoKey)
		}
		// get key
		if major(buf[n]) == type6 && buf[n+1] == tagJsonString {
			n += 2
		}
		if major(buf[n]) != type3 {
			panic(ErrorExpectedCborKey)
		}
		ln, j := decodeLength(buf[n:])
		n += j
		m := n + ln
		//fmt.Println("mapIndex", n, m, string(buf[n:m]), buf[start], start)
		if bytes.Compare(part, buf[n:m]) == 0 {
			return start
		}
		p := itemsEnd(buf[m:]) // value
		//fmt.Println("mapIndex", n, m, p, string(buf[n:m]), start)
		n = m + p
	}
	panic(ErrorMalformedDocument)
}

func itemsEnd(buf []byte) int {
	brkstp := hdr(type7, itemBreak)
	mjr, inf := major(buf[0]), info(buf[0])
	if mjr == type0 || mjr == type1 { // integer item
		if inf < info24 {
			return 1
		}
		return (1 << (inf - info24)) + 1

	} else if mjr == type3 { // string item
		ln, j := decodeLength(buf)
		return j + ln

	} else if mjr == type4 && info(buf[0]) == indefiniteLength { // array item
		n := 1 // skip indefiniteLength
		if buf[n] == brkstp {
			return 2
		}
		n += arrayIndex(buf[n:], -1)
		return n + itemsEnd(buf[n:]) + 1 // skip brkstp

	} else if mjr == type5 && info(buf[0]) == indefiniteLength { // map item
		n := 1 // skip indefiniteLength
		if buf[n] == brkstp {
			return 2
		}
		for n < len(buf) {
			if buf[n] == brkstp {
				return n + 1
			}
			n += itemsEnd(buf[n:]) // key
			n += itemsEnd(buf[n:]) // value
		}

	} else if mjr == type7 {
		if inf == simpleTypeNil || inf == simpleTypeFalse ||
			inf == simpleTypeTrue {
			return 1
		} else if inf == flt32 { // item float32
			return 1 + 4
		} else if inf == flt64 { // item float64
			return 1 + 8
		}
		panic(ErrorInvalidDocument)

	} else if mjr == type6 && buf[1] == tagJsonString && major(buf[2]) == type3 {
		ln, j := decodeLength(buf[2:])
		return 2 + j + ln
	}
	panic(ErrorInvalidDocument)
}

func get(doc, cborptr, item []byte) int {
	n, m := lookup(cborptr, doc)
	copy(item, doc[n:m])
	return m - n
}

func set(doc, cborptr, item, newdoc, old []byte) (int, int) {
	n, m := lookup(cborptr, doc)
	copy(newdoc, doc[:n])
	copy(newdoc, item)
	copy(newdoc, doc[m:])
	copy(old, doc[n:m])
	return (n + len(item) + len(doc[m:])), m - n
}

func del(doc, cborptr, newdoc, deleted []byte) (int, int) {
	n, m := lookup(cborptr, doc)
	copy(newdoc, doc[:n])
	copy(newdoc, doc[m:])
	copy(deleted, doc[n:m])
	return n + len(doc[m:]), m - n
}
