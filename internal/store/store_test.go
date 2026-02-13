package store

import "testing"

func TestBlockstorePutAndCount(t *testing.T) {
	d := t.TempDir()
	bs := NewBlockstore(d)
	cid, err := bs.Put([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if cid == "" {
		t.Fatal("empty cid")
	}
	if !bs.Has(cid) {
		t.Fatal("expected block presence")
	}
	n, err := bs.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected count >=1, got %d", n)
	}
}

func TestChainstoreHead(t *testing.T) {
	d := t.TempDir()
	cs := NewChainStore(d)
	if err := cs.SetHead(Head{Height: 42, TipSetKey: "abc", StateRoot: "abc"}); err != nil {
		t.Fatal(err)
	}
	h, err := cs.Head()
	if err != nil {
		t.Fatal(err)
	}
	if h.Height != 42 {
		t.Fatalf("expected 42 got %d", h.Height)
	}
}
