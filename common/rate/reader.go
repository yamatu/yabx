package rate

import (
	"github.com/juju/ratelimit"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
)

type Reader struct {
	reader  buf.Reader
	limiter *ratelimit.Bucket
}

func NewRateLimitReader(reader buf.Reader, limiter *ratelimit.Bucket) buf.Reader {
	return &Reader{
		reader:  reader,
		limiter: limiter,
	}
}

func (r *Reader) Close() error {
	return common.Close(r.reader)
}

func (r *Reader) Interrupt() {
	common.Interrupt(r.reader)
}

func (r *Reader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := r.reader.ReadMultiBuffer()
	if mb != nil && r.limiter != nil {
		r.limiter.Wait(int64(mb.Len()))
	}
	return mb, err
}
