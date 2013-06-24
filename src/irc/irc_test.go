package irc

import (
	"testing"
	"fmt"
)

func TestIsChannel(t *testing.T) {
	if IsChannel("blah") {
		t.Error("\"blah\" should not be channel")
	}
	if IsChannel("") {
		t.Error("\"\" should not be channel")
	}
	if !IsChannel("#blah") {
		t.Error("\"&blah\" should be channel")
	}
	if !IsChannel("&blah") {
		t.Error("\"&blah\" should be channel")
	}
	if IsChannel("[]") {
		t.Error("\"[]\" should not be channel")
	}
}

func TestLower(t *testing.T) {

	x := func (o string, n string) {
		h := Lower(o)
		if n != h {
			t.Errorf("expected %#v, saw %#v", n, h)
		}
	}

	x("test", "test")
	x("TEST", "test")
	x("Test", "test")
	x("Test[]", "test{}")
	x("Test{}", "test{}")
	x("\\", "|")
	x("~", "^")
}

func TestFromstringer(t *testing.T) {
	f := From{"mynick", "myserver", "myuser", "myhost"}
	if h, n := fmt.Sprintf("%#v", f), `From(nick="mynick", server="myserver", user="myuser", host="myhost")`; h != n {
		t.Errorf("GoString returned %#v, expected %#v", h, n)
	}
}
