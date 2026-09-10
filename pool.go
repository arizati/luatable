package luatable

import "sync"

// ParserPool is a pool of reusable Parser instances. It is safe for concurrent
// use and is intended to avoid allocating a Parser for every short-lived parse.
type ParserPool struct {
	pool sync.Pool
}

// Get returns a Parser from the pool, allocating a new one when the pool is
// empty. The returned parser is reset to its default configuration.
func (pp *ParserPool) Get() *Parser {
	if p, ok := pp.pool.Get().(*Parser); ok {
		return p
	}
	return &Parser{}
}

// Put returns p to the pool. The parser is cleared before being stored so that
// it is not kept alive by references to previously parsed input.
func (pp *ParserPool) Put(p *Parser) {
	if p == nil {
		return
	}
	*p = Parser{}
	pp.pool.Put(p)
}
