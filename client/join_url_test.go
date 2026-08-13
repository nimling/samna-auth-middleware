package client

import "testing"

func TestJoinURL_RelativeSegmentStaysUnderBase(t *testing.T) {
	got, err := joinURL("https://auth.example.io/sam", "user/token")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://auth.example.io/sam/user/token"
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
}

func TestJoinURL_AbsoluteSegmentDropsBasePath(t *testing.T) {
	got, err := joinURL("https://auth.example.io/sam", "/api/app/docs/subscription/client")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://auth.example.io/api/app/docs/subscription/client"
	if got != want {
		t.Fatalf("want %s, got %s", want, got)
	}
}
