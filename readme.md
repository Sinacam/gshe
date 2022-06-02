# GSHE
GSHE is a homomorphic encryption library for grayscale images in Go. GSHE also contains a command line interface in the subdirectory `app`. The library API documentation is available by `go doc`.

## CLI Usage
```
app [options] input_file
  -c    compress mode
  -d    decrypt mode
  -e    encrypt mode
  -f    force overwrite existing files
  -k string
        path to key file
  -o string
        path to output file
  -p string
        passkey
  -q uint
        quantization for compression (default 1)
```

If no mode is supplied, then the mode is inferred from the input file extension. One of key file or passkey must be provided for encryption and decryption.