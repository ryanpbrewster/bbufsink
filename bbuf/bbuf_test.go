package bbuf_test

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"rpb.dev/bbufsink/bbuf"
)

func Test_ReadMyWrites(t *testing.T) {
	b := bbuf.New(10)

	w, err := b.Reserve(4)
	if err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
	copy(w, []byte("abcd"))

	if err := b.Commit(4); err != nil {
		t.Fatalf("b.Commit: %v", err)
	}

	r := b.Read()
	if got, want := r, "abcd"; !bytes.Equal(got, []byte(want)) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if err := b.Release(4); err != nil {
		t.Fatalf("b.Release: %v", err)
	}
}

func Test_InterleavedReadsAndWrites(t *testing.T) {
	b := bbuf.New(10)

	// Write 4 bytes
	w1, err := b.Reserve(4)
	if err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
	copy(w1, []byte("aaaa"))
	if err := b.Commit(4); err != nil {
		t.Fatalf("b.Commit: %v", err)
	}

	// Read 4 bytes, but don't release it yet
	r1 := b.Read()

	w2, err := b.Reserve(4)
	if err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
	copy(w2, []byte("bbbb"))
	if err := b.Commit(4); err != nil {
		t.Fatalf("b.Commit: %v", err)
	}

	// Now check r1 after we're written new data. It should still be valid.
	if got, want := r1, []byte("aaaa"); !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := b.Release(4); err != nil {
		t.Fatalf("b.Release: %v", err)
	}

	// And another read should see "bbbb"
	r2 := b.Read()
	if got, want := r2, []byte("bbbb"); !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := b.Release(4); err != nil {
		t.Fatalf("b.Release: %v", err)
	}
}

func Test_Wraparound(t *testing.T) {
	b := bbuf.New(10)

	// Write & release 5 bytes
	w1, err := b.Reserve(5)
	if err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
	copy(w1, []byte("aaaaa"))
	if err := b.Commit(4); err != nil {
		t.Fatalf("b.Commit: %v", err)
	}
	r1 := b.Read()
	if got, want := r1, []byte("aaaaa"); !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := b.Release(len(r1)); err != nil {
		t.Fatalf("b.Release: %v", err)
	}

	// Now write 4 bytes, twice. That should wrap us around the end of the buffer.
	w2, err := b.Reserve(4)
	if err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
	copy(w2, []byte("bbbb"))
	if err := b.Commit(4); err != nil {
		t.Fatalf("b.Commit: %v", err)
	}
	w3, err := b.Reserve(4)
	if err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
	copy(w3, []byte("cccc"))
	if err := b.Commit(4); err != nil {
		t.Fatalf("b.Commit: %v", err)
	}

	// Because it wrapped around, the reads will necessarily be split.
	r2 := b.Read()
	if got, want := r2, []byte("bbbb"); !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := b.Release(len(r2)); err != nil {
		t.Fatalf("b.Release: %v", err)
	}

	r3 := b.Read()
	if got, want := r3, []byte("cccc"); !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if err := b.Release(len(r3)); err != nil {
		t.Fatalf("b.Release: %v", err)
	}
}

func Test_OutOfSpace_EdgeCases(t *testing.T) {
	b := bbuf.New(10)

	// We don't allow completely filling the buffer
	if _, err := b.Reserve(10); err == nil {
		t.Fatalf("b.Reserve: expected err")
	}

	// 9/10 is allowed
	w1, err := b.Reserve(9)
	if err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
	payload1 := bytes.Repeat([]byte("a"), len(w1))
	copy(w1, payload1)
	if err := b.Commit(len(w1)); err != nil {
		t.Fatalf("b.Commit: %v", err)
	}
	if got, want := b.Read(), payload1; !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// But now the buffer is "split" and you can't write 9/10 again
	if _, err := b.Reserve(9); err == nil {
		t.Fatalf("b.Reserve: expected err")
	}

	// 8/10 is allowed
	if _, err := b.Reserve(8); err != nil {
		t.Fatalf("b.Reserve: %v", err)
	}
}

func Test_Write1kEntries(t *testing.T) {
	b := bbuf.New(37)
	actual := new(bytes.Buffer)
	expected := new(bytes.Buffer)

	drain := func() {
		for r := b.Read(); r != nil; r = b.Read() {
			actual.Write(r)
			b.Release(len(r))
		}
	}
	for i := 0; i < 1_000; i++ {
		payload := []byte(fmt.Sprintf("payload-%d\n", i))
		expected.Write(payload)

		w, err := b.Reserve(len(payload))
		if errors.Is(err, bbuf.ErrNotEnoughSpace) {
			drain()
			w, err = b.Reserve(len(payload))
		}
		if err != nil {
			t.Fatalf("b.Reserve(%d): %v", len(payload), err)
		}
		copy(w, payload)
		if err := b.Commit(len(payload)); err != nil {
			t.Fatalf("b.Commit: %v", err)
		}
	}
	fmt.Printf("[RPB] done buffering: %+v\n", b)
	drain()
	fmt.Printf("[RPB] final drain: %+v\n", b)

	if got, want := actual.String(), expected.String(); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got, want := actual.Len(), expected.Len(); got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
