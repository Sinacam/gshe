package main

import (
	"encoding/base64"
	"encoding/gob"
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sinacam/gshe"
)

var config struct {
	// flag vars
	keyPath                    string
	inPath, outPath            string
	encrypt, compress, decrypt bool
	overwrite                  bool
	quantization               uint
	key                        string

	mode int // stores the boolean mode flags as integer
}

const (
	modeEncrypt = iota + 1
	modeCompress
	modeDecrypt
)

func main() {
	flag.StringVar(&config.outPath, "o", "", "path to output file")
	flag.StringVar(&config.keyPath, "k", "", "path to key file")
	flag.StringVar(&config.key, "p", "", "passkey")
	flag.UintVar(&config.quantization, "q", 1, "quantization for compression")
	flag.BoolVar(&config.encrypt, "e", false, "encrypt mode")
	flag.BoolVar(&config.compress, "c", false, "compress mode")
	flag.BoolVar(&config.decrypt, "d", false, "decrypt mode")
	flag.BoolVar(&config.overwrite, "f", false, "force overwrite existing files")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [options] input_file\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Fprintln(os.Stderr, "no input file specified")
		flag.Usage()
		return
	}

	config.inPath = flag.Arg(0)
	name := filepath.Base(config.inPath)
	ext := filepath.Ext(name)

	modes := 0
	if config.encrypt {
		modes++
		config.mode = modeEncrypt
	}
	if config.decrypt {
		modes++
		config.mode = modeDecrypt
	}
	if config.compress {
		modes++
		config.mode = modeCompress
	}
	if modes > 1 {
		fmt.Fprintln(os.Stderr, "multiple modes specified")
		flag.Usage()
		return
	}

	// infer mode if none is set
	if config.mode == 0 {
		switch ext {
		case ".gse":
			config.mode = modeCompress
		case ".gsc":
			config.mode = modeDecrypt
		case ".png":
			fallthrough
		case ".gif":
			fallthrough
		case ".jpg":
			fallthrough
		case ".jpeg":
			config.mode = modeEncrypt
		default:
			fmt.Fprintln(os.Stderr, "unknown file type")
			flag.Usage()
			return
		}
	}

	// default output is same path with extension changed
	if config.outPath == "" {
		name = name[:len(name)-len(ext)]
		outext := ""
		switch config.mode {
		case modeEncrypt:
			outext = "gse"
		case modeCompress:
			outext = "gsc"
		case modeDecrypt:
			outext = "png"
		}
		config.outPath = filepath.Join(filepath.Dir(config.inPath), fmt.Sprintf("%v.%v", name, outext))
	}

	if config.mode == modeEncrypt || config.mode == modeDecrypt {
		if config.key != "" && config.keyPath != "" {
			fmt.Fprintln(os.Stderr, "two passkeys provided")
			flag.Usage()
			return
		}
		if config.key == "" && config.keyPath == "" {
			fmt.Fprintln(os.Stderr, "no passkeys provided")
			flag.Usage()
			return
		}

		if config.keyPath != "" {
			key, err := readKey(config.keyPath)
			if err != nil {
				fmt.Fprintln(os.Stderr, "invalid key file: ", err)
				flag.Usage()
				return
			}
			config.key = string(key)
		}
	}

	if config.quantization > 255 {
		fmt.Fprintln(os.Stderr, "invalid quantization")
		flag.Usage()
		return
	}

	if !config.overwrite {
		if _, err := os.Stat(config.outPath); err == nil {
			fmt.Printf("Overwrite existing file %v? (y/[n]): ", config.outPath)
			s := ""
			fmt.Scanln(&s)
			s = strings.ToLower(s)
			switch s {
			case "y":
			case "yes":
			default:
				return
			}
		}
	}

	switch config.mode {
	case modeEncrypt:
		src, err := readGray(config.inPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		img, err := imageFromGray(src)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		fmt.Printf("width: %v height: %v\n", img.Width, img.Height)

		enc, err := gshe.Encrypt(img, []byte(config.key))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}

		outfile, err := os.OpenFile(config.outPath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer outfile.Close()
		if err := gob.NewEncoder(outfile).Encode(enc); err != nil {
			fmt.Println(err)
			return
		}

	case modeCompress:
		enc := &gshe.EncryptedImage{}
		infile, err := os.Open(config.inPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer infile.Close()
		if err := gob.NewDecoder(infile).Decode(enc); err != nil {
			fmt.Println(err)
			return
		}

		comp, err := gshe.Compress(enc, uint8(config.quantization))
		if err != nil {
			fmt.Println(err)
			return
		}

		originalSize := comp.Height * comp.Width
		compressedSize := len(comp.Qtable) + len(comp.EncQdiffs) + len(comp.Quarterimage)
		ratio := float64(compressedSize) / float64(originalSize)
		fmt.Printf("q: %v orig: %6dk diffs: %6dk comp: %6dk ratio: %.3f\n",
			config.quantization, originalSize/1000, len(comp.EncQdiffs)/1000, compressedSize/1000, ratio)

		outfile, err := os.OpenFile(config.outPath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer outfile.Close()
		if err := gob.NewEncoder(outfile).Encode(comp); err != nil {
			fmt.Println(err)
			return
		}

	case modeDecrypt:
		comp := &gshe.CompressedImage{}
		infile, err := os.Open(config.inPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return
		}
		defer infile.Close()
		if err := gob.NewDecoder(infile).Decode(comp); err != nil {
			fmt.Println(err)
			return
		}

		dec, err := gshe.Decrypt(comp, []byte(config.key))
		if err != nil {
			fmt.Println(err)
			return
		}

		img := grayFromImage(dec)
		outfile, err := os.OpenFile(config.outPath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Println(err)
			return
		}
		defer outfile.Close()
		if err := png.Encode(outfile, img); err != nil {
			fmt.Println(err)
			return
		}
	}
}

func readGray(path string) (*image.Gray, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	m := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(m, m.Bounds(), src, b.Min, draw.Src)

	return m, nil
}

func imageFromGray(img *image.Gray) (*gshe.Image, error) {
	return gshe.NewImage(img.Pix, img.Rect.Dx(), img.Rect.Dy())
}

func grayFromImage(img *gshe.Image) *image.Gray {
	dw := 0
	dh := 0
	if img.PadWidth {
		dw = -1
	}
	if img.PadHeight {
		dh = -1
	}

	g := image.NewGray(image.Rect(0, 0, img.Width+dw, img.Height+dh))
	g.Pix = img.Image
	return g
}

func readKey(path string) ([]byte, error) {
	src, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	dec := base64.NewDecoder(base64.StdEncoding, src)
	return ioutil.ReadAll(dec)
}
