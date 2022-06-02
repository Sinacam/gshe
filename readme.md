# GSHE
GSHE is a homomorphic encryption library for grayscale images in Go. It is an implementation of ["A new lossy compression scheme for encrypted gray-scale images"][2]. GSHE allows an image to be first encrypted, then compressed, and finally decrypted/decompressed, where the three steps can be performed by different parties. GSHE also contains a command line interface in the subdirectory `app`. The library API documentation is available by `go doc`.

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

If no mode is supplied, then the mode is inferred from the input file extension.

One of key file or passkey must be provided for encryption and decryption. The key file is a standard base64 encoded (defined in [RFC 4648][1]) file of arbitrary length. The passkey is any string of arbitrary length.

It is recommended to use quantization `1` unless possible large distortions can be tolerated.

[1]: https://www.rfc-editor.org/rfc/rfc4648.html
[2]: https://ieeexplore.ieee.org/document/6855035