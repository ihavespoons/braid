package runner

import (
	"reflect"
	"testing"
)

func TestLineBuffer_SingleCompleteLine(t *testing.T) {
	b := &LineBuffer{}
	lines := b.Push("hello\n")
	if !reflect.DeepEqual(lines, []string{"hello"}) {
		t.Errorf("got %v, want [hello]", lines)
	}
	if flushed := b.Flush(); flushed != nil {
		t.Errorf("flush should be empty, got %v", flushed)
	}
}

func TestLineBuffer_SplitAcrossChunks(t *testing.T) {
	b := &LineBuffer{}
	if lines := b.Push("hel"); lines != nil {
		t.Errorf("partial chunk should emit nothing, got %v", lines)
	}
	if lines := b.Push("lo\nwor"); !reflect.DeepEqual(lines, []string{"hello"}) {
		t.Errorf("got %v, want [hello]", lines)
	}
	if lines := b.Push("ld\n"); !reflect.DeepEqual(lines, []string{"world"}) {
		t.Errorf("got %v, want [world]", lines)
	}
	if flushed := b.Flush(); flushed != nil {
		t.Errorf("flush should be empty after complete lines, got %v", flushed)
	}
}

func TestLineBuffer_MultipleLinesInOneChunk(t *testing.T) {
	b := &LineBuffer{}
	lines := b.Push("a\nb\nc\n")
	if !reflect.DeepEqual(lines, []string{"a", "b", "c"}) {
		t.Errorf("got %v, want [a b c]", lines)
	}
}

func TestLineBuffer_FlushPartialLine(t *testing.T) {
	b := &LineBuffer{}
	_ = b.Push("partial")
	flushed := b.Flush()
	if !reflect.DeepEqual(flushed, []string{"partial"}) {
		t.Errorf("got %v, want [partial]", flushed)
	}
	// Flush should reset the buffer.
	if flushed := b.Flush(); flushed != nil {
		t.Errorf("second flush should be empty, got %v", flushed)
	}
}

func TestLineBuffer_EmptyChunks(t *testing.T) {
	b := &LineBuffer{}
	if lines := b.Push(""); lines != nil {
		t.Errorf("empty chunk should emit nothing, got %v", lines)
	}
	if flushed := b.Flush(); flushed != nil {
		t.Errorf("flush on empty buffer should return nothing, got %v", flushed)
	}
}

func TestLineBuffer_ConsecutiveNewlines(t *testing.T) {
	b := &LineBuffer{}
	lines := b.Push("a\n\nb\n")
	if !reflect.DeepEqual(lines, []string{"a", "", "b"}) {
		t.Errorf("got %v, want [a  b]", lines)
	}
}
