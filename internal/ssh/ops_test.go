package ssh

import "testing"

func TestClassifyMode(t *testing.T) {
	cases := []struct {
		mode uint32
		want FileType
	}{
		{0o040755, FileTypeDirectory},
		{0o100644, FileTypeFile},
		{0o120777, FileTypeSymlink},
		{0o010644, FileTypeOther}, // FIFO
		{0o020644, FileTypeOther}, // char device
	}
	for _, c := range cases {
		if got := classifyMode(c.mode); got != c.want {
			t.Errorf("classifyMode(%o) = %s, want %s", c.mode, got, c.want)
		}
	}
}
