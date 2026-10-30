package qpack

import "io"

// DecodeFull keeps the v0.5 API for github.com/sagernet/quic-go/http3, which
// shares this module with github.com/apernet/quic-go/http3 (v0.6 API).
func (d *Decoder) DecodeFull(p []byte) ([]HeaderField, error) {
	hfs := []HeaderField{}
	if len(p) == 0 {
		return hfs, nil
	}
	next := d.Decode(p)
	for {
		hf, err := next()
		if err == io.EOF {
			return hfs, nil
		}
		if err != nil {
			return nil, err
		}
		hfs = append(hfs, hf)
	}
}
