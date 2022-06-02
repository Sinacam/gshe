package gshe

import (
	"bytes"
	"math/rand"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	genSalt = func() ([]byte, error) {
		return make([]byte, 16), nil
	}
	os.Exit(m.Run())
}

func TestMinMaxMedian(t *testing.T) {
	// These are all 24 permutations possible
	ss := []string{"1234", "1243", "2134", "2143", "1324", "1342",
		"3124", "3142", "1423", "1432", "4123", "4132",
		"2314", "2341", "3214", "3241", "2413", "2431",
		"4213", "4231", "3412", "3421", "4312", "4321"}
	for _, s := range ss {
		min, max, median := minmaxmedian([4]byte{s[0], s[1], s[2], s[3]})
		if min != '1' || max != '4' || (median != '2' && median != '3') {
			t.Fatalf("min=%v max=%v median=%v", min, max, median)
		}
	}
}

func TestPermute(t *testing.T) {
	key := []byte("I am probably a secretive secret")
	salt, _ := genSalt()

	payload := "Do I look like half an image to you?"
	halfimage := []byte(payload)
	rng := rand.New(source{newRNG(key, salt)})
	permuteHalfimage(halfimage, rng)

	blocks := make([][4]byte, len(halfimage)/2)
	for i := range blocks {
		blocks[i][0] = halfimage[2*i]
		blocks[i][1] = halfimage[2*i+1]
	}
	rng = rand.New(source{newRNG(key, salt)})
	blocks = unpermuteBlocks(blocks, rng)

	got := make([]byte, len(halfimage))
	for i := range blocks {
		got[2*i] = blocks[i][0]
		got[2*i+1] = blocks[i][1]
	}

	if string(got) != payload {
		t.Fatalf("\nexpect: %v\ngot: %v", payload, string(got))
	}
}

func TestEncrypt(t *testing.T) {
	key := []byte("I am probably a secretive secret")

	payload := "Do I look like a real image to you??"
	img, err := NewImage([]byte(payload), 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt(img, key)
	if err != nil {
		t.Fatal(err)
	}

	// This test relies on the particular rng used
	expect := []byte{38, 38, 52, 68, 154, 144, 96, 43, 161, 238, 157, 181, 107, 150, 223, 40, 236, 236}
	if !bytes.Equal(enc.Halfimage, expect) {
		t.Fatalf("\nexpect: %v\ngot: %v", expect, enc.Halfimage)
	}
}

func TestCompressQuarterimage(t *testing.T) {
	key := []byte("I am probably a secretive secret")

	payload := "Do I look like a real image to you??"
	img, err := NewImage([]byte(payload), 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt(img, key)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := compress(enc, 1)
	if err != nil {
		t.Fatal(err)
	}

	// This test relies on the particular rng used
	expect := []byte{38, 52, 154, 96, 161, 157, 107, 223, 236}
	if !bytes.Equal(comp.Quarterimage, expect) {
		t.Fatalf("\nexpect: %v\ngot: %v", expect, comp.Quarterimage)
	}
}

func TestCompressDiffs(t *testing.T) {
	key := []byte("I am probably a secretive secret")

	payload := "Do I look like a real image to you??"
	img, err := NewImage([]byte(payload), 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt(img, key)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := compress(enc, 1)
	if err != nil {
		t.Fatal(err)
	}

	// This test relies on the particular rng used
	expect := []byte{0, 16, 246, 203, 77, 24, 43, 73, 0}
	if !bytes.Equal(comp.Qdiffs, expect) {
		t.Fatalf("\nexpect: %v\ngot: %v", expect, comp.Qdiffs)
	}
}

func TestDecryptHalfimage(t *testing.T) {
	key := []byte("I am probably a secretive secret")
	// With quantization 1, the half image is perfectly reconstructed.
	payload := "Do I look like a real image to you??"
	img, err := NewImage([]byte(payload), 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt(img, key)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := compress(enc, 1)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := decrypt(comp, key)
	if err != nil {
		t.Fatal(err)
	}

	strides := []int{2, 2, 3, 2, 2, 1}
	expect := strided([]byte(payload), strides)
	got := strided(dec.Image, strides)
	if !bytes.Equal(expect, got) {
		t.Fatalf("\nexpect: %v\ngot:    %v", string(expect), string(got))
	}
}

func TestDecryptGradient(t *testing.T) {
	key := []byte("I am probably a secretive secret")
	// With quantization 1, CAI should perfectly reconstruct a gradient in one direction
	// The border will have some trouble, so we leave them the same as the neighbouring row.
	// The gradient has to be stronger than the CAI threshold, otherwise the gradient will
	// not be treated as an edge.
	g := byte(21)
	payload := make([]byte, 256)
	for y := 1; y < 15; y++ {
		for x := 0; x < 16; x++ {
			payload[y*16+x] = byte(y) * g // horizontal stripes of g,2g,...14g
		}
	}
	for x := 0; x < 16; x++ {
		payload[x] = g
		payload[15*16+x] = 14 * g
	}

	img, err := NewImage([]byte(payload), 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt(img, key)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := compress(enc, 1)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := decrypt(comp, key)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(dec.Image, payload) {
		t.Fatal()
	}
}

// TestFullDecryptGradient also tests for fselib encoding/decoding.
// Otherwise identical to TestDecryptGradient.
func TestFullDecryptGradient(t *testing.T) {
	key := []byte("I am probably a secretive secret")
	// With quantization 1, CAI should perfectly reconstruct a gradient in one direction
	// The border will have some trouble, so we leave them the same as the neighbouring row.
	// The gradient has to be stronger than the CAI threshold, otherwise the gradient will
	// not be treated as an edge.
	g := byte(21)
	payload := make([]byte, 256)
	for y := 1; y < 15; y++ {
		for x := 0; x < 16; x++ {
			payload[y*16+x] = byte(y) * g // horizontal stripes of g,2g,...14g
		}
	}
	for x := 0; x < 16; x++ {
		payload[x] = g
		payload[15*16+x] = 14 * g
	}

	img, err := NewImage([]byte(payload), 16, 16)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encrypt(img, key)
	if err != nil {
		t.Fatal(err)
	}
	comp, err := Compress(enc, 1)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := Decrypt(comp, key)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(dec.Image, payload) {
		t.Fatal()
	}
}

// Collects elements from p each seperated by a distance specified by strides.
// The last element from strides is not collected.
// This repeats until end of p is reached.
// Requires len(p) % sum(strides) == 0, use the last element to pad strides to desired sum.
func strided(p []byte, strides []int) []byte {
	ret := make([]byte, 0)
	for len(p) > 0 {
		for s := strides; len(s) > 1; s = s[1:] {
			ret = append(ret, p[0])
			p = p[s[0]:]
		}
		p = p[strides[len(strides)-1]:]
	}
	return ret
}
