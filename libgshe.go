package gshe

import (
	"errors"
	"io"
	"math/bits"
	"math/rand"

	fselib "github.com/Sinacam/gshe/FiniteStateEntropy/lib"
)

// TODO: change Image to image.Gray
// Image is a greyscale image that is possibly padded in width and height
// to a multiple of 2 with zeros.
type Image struct {
	Image               []byte
	Width, Height       int
	PadWidth, PadHeight bool // whether the image was padded
}

// NewImage creates new image and possibly pads width and height
// to a multiple of 2 with zeros.
func NewImage(data []byte, width, height int) (*Image, error) {
	if len(data) != width*height {
		return nil, errors.New("invalid image data")
	}

	if width%2 == 0 && height%2 == 0 {
		return &Image{
			Image:  data,
			Width:  width,
			Height: height,
		}, nil
	}

	pw := width + width%2
	ph := height + height%2
	padded := make([]byte, 0, pw*ph)
	if width%2 != 0 {
		for y := 0; y < height; y++ {
			copy(padded[y*pw:], data[y*width:(y+1)*width])
		}
	}

	return &Image{
		Image:     padded,
		Width:     pw,
		Height:    ph,
		PadWidth:  width%2 != 0,
		PadHeight: height%2 != 0,
	}, nil
}

func (img *Image) At(x, y int) byte {
	return img.Image[y*img.Width+x]
}

// source is a shim for math/rand.Source
type source struct {
	r io.Reader
}

func (src source) Int63() int64 {
	buf := make([]byte, 8)
	src.r.Read(buf)
	return int64(buf[0]&0x7f)<<56 | int64(buf[1])<<48 | int64(buf[2])<<40 | int64(buf[3])<<32 |
		int64(buf[4])<<24 | int64(buf[5])<<16 | int64(buf[6])<<8 | int64(buf[7])
}

func (src source) Seed(seed int64) {
	return
}

type EncryptedImage struct {
	Halfimage           []byte
	Width, Height       int
	PadWidth, PadHeight bool   // whether the image was padded
	Salt                []byte // salt used in encryption
}

// Encrypts the image img using secret key.
// secret should be either 16, 24, or 32 bytes to select AES-128, AES-192, or AES-256.
func Encrypt(img *Image, key []byte) (*EncryptedImage, error) {
	salt, err := genSalt()
	if err != nil {
		return nil, err
	}
	rng := newRNG(key, salt)

	mask := make([]byte, len(img.Image)/4)
	rng.Read(mask)
	maskAt := func(x, y int) byte {
		// int division by 2 is 3 instructions while uint is 1
		// I hate this more than you'd think
		i := uint(y)/2*uint(img.Width)/2 + uint(x)/2
		return mask[int(i)]
	}

	// halfimage is stored in block order, i.e.
	// 		halfimage[0] is pixel (0, 0)
	// 		halfimage[1] is pixel (1, 1)
	// 		halfimage[2] is pixel (2, 0)
	halfimage := make([]byte, len(img.Image)/2)
	halfimageAt := func(x, y int) *byte {
		return &halfimage[int(uint(y)/2)*img.Width+x]
	}

	for y := 0; y < img.Height; y += 2 {
		for x := 0; x < img.Width; x += 2 {
			*halfimageAt(x, y) = img.At(x, y) + maskAt(x, y)
			*halfimageAt(x+1, y+1) = img.At(x+1, y+1) + maskAt(x, y)
		}
	}

	permuteHalfimage(halfimage, rand.New(source{rng}))

	return &EncryptedImage{
		Halfimage: halfimage,
		Salt:      salt,
		Width:     img.Width,
		Height:    img.Height,
		PadWidth:  img.PadHeight,
		PadHeight: img.PadHeight,
	}, nil
}

// permutes the half image p consisting of the top left and bottom right pixels
// of each 2x2 blocks with rng.
func permuteHalfimage(p []byte, rng *rand.Rand) {
	for ; len(p) > 0; p = p[2:] {
		n := rng.Intn(len(p)/2) * 2
		p[0], p[n] = p[n], p[0]
		p[1], p[n+1] = p[n+1], p[1]
	}
}

type CompressedImage struct {
	Quarterimage        []byte
	Qtable              []byte // quantization table
	EncQdiffs           []byte // encoded quantized differences, i.e. indexes into Qtable
	Salt                []byte // salt used in encryption
	Width, Height       int
	PadWidth, PadHeight bool // whether the image was padded
}

// Same as CompressedImage, but without encoding qdiffs.
// Used as an intermediary step.
type compressedImage struct {
	Quarterimage        []byte
	Qtable              []byte // quantization table
	Qdiffs              []byte // quantized differences, i.e. indexes into Qtable
	Salt                []byte // salt used in encryption
	Width, Height       int
	PadWidth, PadHeight bool // whether the image was padded
}

func makeQtable(distortions []int, quantization uint8) []byte {
	logq := byte(bits.TrailingZeros8(quantization))
	qtable := make([]byte, 256>>logq)
	for k := range qtable {
		qtable[k] = byte(k) << logq
		for j := byte(1); j < quantization; j++ {
			if distortions[j] < distortions[qtable[k]] {
				qtable[k] = (byte(k) << logq) + j
			}
		}
	}
	return qtable
}

// This is the entire compression except without fselib encoding.
func compress(img *EncryptedImage, quantization uint8) (*compressedImage, error) {
	// Quantization creates disproportionate distortions
	// due to unsigned arithmetic overflowing 255 or underflowing 0.
	// However, this cannot be solved because the pixel values are masked,
	// and the unmasking may cause the overflow or underflow.
	diffs := make([]byte, len(img.Halfimage)/2)
	for i := 0; i < len(img.Halfimage); i += 2 {
		diffs[i/2] = img.Halfimage[i+1] - img.Halfimage[i]
	}

	if bits.OnesCount8(quantization) != 1 {
		return nil, errors.New("quantization must be power of 2")
	}

	distortions := make([]int, 256)
	logq := bits.TrailingZeros8(quantization)
	maskq := quantization - 1
	for _, v := range diffs {
		k := v >> logq
		i := k << logq
		for j := byte(0); j < quantization; j++ {
			distortion := (v - j) & maskq
			distortions[i+j] += int(distortion * distortion)
		}
	}

	qdiffs := make([]byte, len(diffs))
	for i, v := range diffs {
		k := v >> logq
		qdiffs[i] = k
	}

	quarterimage := make([]byte, len(diffs))
	for i := range quarterimage {
		quarterimage[i] = img.Halfimage[2*i]
	}

	return &compressedImage{
		Quarterimage: quarterimage,
		Qtable:       makeQtable(distortions, quantization),
		Qdiffs:       qdiffs,
		Salt:         img.Salt,
		Width:        img.Width,
		Height:       img.Height,
		PadWidth:     img.PadHeight,
		PadHeight:    img.PadHeight,
	}, nil
}

// quantization must be a power of 2.
func Compress(img *EncryptedImage, quantization uint8) (*CompressedImage, error) {
	comp, err := compress(img, quantization)
	if err != nil {
		return nil, err
	}

	encqdiffs := make([]byte, len(comp.Qdiffs))
	n, err := fselib.Encode(encqdiffs, comp.Qdiffs)
	if err != nil {
		return nil, err
	}

	return &CompressedImage{
		Quarterimage: comp.Quarterimage,
		Qtable:       comp.Qtable,
		EncQdiffs:    encqdiffs[:n],
		Salt:         img.Salt,
		Width:        comp.Width,
		Height:       comp.Height,
		PadWidth:     comp.PadWidth,
		PadHeight:    comp.PadHeight,
	}, nil
}

func Decrypt(img *CompressedImage, key []byte) (*Image, error) {
	qdiffs := make([]byte, len(img.Quarterimage))
	n, err := fselib.Decode(qdiffs, img.EncQdiffs)
	if err != nil {
		return nil, err
	}
	qdiffs = qdiffs[:n]

	return decrypt(&compressedImage{
		Quarterimage: img.Quarterimage,
		Qtable:       img.Qtable,
		Qdiffs:       qdiffs,
		Salt:         img.Salt,
		Width:        img.Width,
		Height:       img.Height,
		PadWidth:     img.PadHeight,
		PadHeight:    img.PadHeight,
	}, key)
}

// This is the entire decryption except without fselib decoding.
func decrypt(img *compressedImage, key []byte) (*Image, error) {
	blocks := make([][4]byte, len(img.Quarterimage))

	for i := range blocks {
		blocks[i][0] = img.Quarterimage[i]
		blocks[i][3] = img.Quarterimage[i] + img.Qtable[img.Qdiffs[i]]
	}

	rng := newRNG(key, img.Salt)

	mask := make([]byte, len(img.Quarterimage))
	rng.Read(mask)

	blocks = unpermuteBlocks(blocks, rand.New(source{rng}))

	for i, v := range mask {
		blocks[i][0] -= v
		blocks[i][3] -= v
	}

	// TODO: compute threshold from image complexity
	threshold := 20
	bw := img.Width / 2
	bh := img.Height / 2
	interpolateBlocks(blocks, bw, bh, threshold)

	image := make([]byte, len(img.Quarterimage)*4)
	imageAt := func(x, y int) *byte {
		return &image[y*img.Width+x]
	}
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			*imageAt(2*x, 2*y) = blocks[y*bw+x][0]
			*imageAt(2*x+1, 2*y) = blocks[y*bw+x][1]
			*imageAt(2*x, 2*y+1) = blocks[y*bw+x][2]
			*imageAt(2*x+1, 2*y+1) = blocks[y*bw+x][3]
		}
	}

	return &Image{
		Image:     image,
		Width:     img.Width,
		Height:    img.Height,
		PadWidth:  img.PadWidth,
		PadHeight: img.PadHeight,
	}, nil
}

// unpermute the 2x2 blocks according to rng, which must match the state used
// in permuteHalfimage.
// Does not modify blocks and returns the unpermuted blocks.
func unpermuteBlocks(blocks [][4]byte, rng *rand.Rand) [][4]byte {
	indices := make([]int, len(blocks))
	for i := range indices {
		indices[i] = i
	}
	for s := indices; len(s) > 0; s = s[1:] {
		n := rng.Intn(len(s))
		s[0], s[n] = s[n], s[0]
	}

	ret := make([][4]byte, len(blocks))
	for i, v := range indices {
		ret[v] = blocks[i]
	}
	return ret
}

// Interpolates the blocks with cai.
// Performs some heuristics along the border of the image.
func interpolateBlocks(blocks [][4]byte, bw, bh, threshold int) {
	for y := 0; y < bh; y++ {
		for x := 0; x < bw; x++ {
			i := y*bw + x
			up := (y-1)*bw + x
			right := y*bw + x + 1
			down := (y+1)*bw + x
			left := y*bw + x - 1

			// On the image border, there are pixels without 4 neighbours.
			// Leaving these neighbours as 0 creates undesirable artifacts.
			// Here we heuristically choose values from the current block.
			neighbors := [4]byte{0, 0, blocks[i][3], blocks[i][0]}
			if y > 0 {
				neighbors[0] = blocks[up][3]
			} else {
				neighbors[0] = blocks[i][3]
			}
			if x < bw-1 {
				neighbors[1] = blocks[right][0]
			} else {
				neighbors[1] = blocks[i][0]
			}
			blocks[i][1] = cai(neighbors, threshold)

			neighbors = [4]byte{blocks[i][0], blocks[i][3]}
			if y < bh-1 {
				neighbors[2] = blocks[down][0]
			} else {
				neighbors[2] = blocks[i][0]
			}
			if x > 0 {
				neighbors[3] = blocks[left][3]
			} else {
				neighbors[3] = blocks[i][3]
			}
			blocks[i][2] = cai(neighbors, threshold)
		}
	}
}

// Context Adaptive Interpolation.
// Neighbors are ordered clockwise, starting with the top pixel
func cai(neighbors [4]byte, threshold int) byte {
	min, max, median := minmaxmedian(neighbors)

	// returned values are all rounded to nearest
	if int(max)-int(min) <= threshold {
		sum := int(neighbors[0]) + int(neighbors[1]) + int(neighbors[2]) + int(neighbors[3])
		return byte((sum + 2) / 4)
	}
	if absdiff(neighbors[1], neighbors[3])-absdiff(neighbors[0], neighbors[2]) > threshold {
		sum := int(neighbors[0]) + int(neighbors[2])
		return byte((sum + 1) / 2)
	}
	if absdiff(neighbors[0], neighbors[2])-absdiff(neighbors[1], neighbors[3]) > threshold {
		sum := int(neighbors[1]) + int(neighbors[3])
		return byte((sum + 1) / 2)
	}
	return median
}

func absdiff(x, y byte) int {
	if x > y {
		return int(x - y)
	}
	return int(y - x)
}

func minmaxmedian(p [4]byte) (byte, byte, byte) {
	b := [4]byte{}

	if p[0] < p[1] {
		b[0], b[1] = p[0], p[1]
	} else {
		b[0], b[1] = p[1], p[0]
	}
	if p[2] < p[3] {
		b[2], b[3] = p[2], p[3]
	} else {
		b[2], b[3] = p[3], p[2]
	}

	if b[0] > b[2] {
		b[0], b[2] = b[2], b[0]
	}
	if b[1] > b[3] {
		b[1], b[3] = b[3], b[1]
	}
	return b[0], b[3], b[1]
}
