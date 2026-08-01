package native

// COBSEncode returns a Consistent Overhead Byte Stuffing representation.
func COBSEncode(src []byte) []byte {
	dst := make([]byte, 1, len(src)+len(src)/254+1)
	codeIndex := 0
	code := byte(1)

	for _, value := range src {
		if value == 0 {
			dst[codeIndex] = code
			codeIndex = len(dst)
			dst = append(dst, 0)
			code = 1
			continue
		}

		dst = append(dst, value)
		code++
		if code == 0xFF {
			dst[codeIndex] = code
			codeIndex = len(dst)
			dst = append(dst, 0)
			code = 1
		}
	}
	dst[codeIndex] = code
	return dst
}

func COBSDecode(src []byte) ([]byte, error) {
	if len(src) == 0 {
		return nil, ErrEmptyFrame
	}

	dst := make([]byte, 0, len(src))
	for index := 0; index < len(src); {
		code := int(src[index])
		if code == 0 {
			return nil, ErrMalformedCOBS
		}
		index++

		count := code - 1
		if index+count > len(src) {
			return nil, ErrMalformedCOBS
		}
		dst = append(dst, src[index:index+count]...)
		index += count

		if code != 0xFF && index < len(src) {
			dst = append(dst, 0)
		}
	}
	return dst, nil
}
