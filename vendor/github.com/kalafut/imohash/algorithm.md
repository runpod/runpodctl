## Introduction

imohash is a file hashing algorithm optimized for large files. It uses
file size and sampling in hash generation. Because it does not process
the whole file, it is not a general purpose hashing algorithm. But for
applications where a hash sample is sufficient, imohash will provide a
high performance hashing, especially for large files over slow
networks.

## Algorithm

imohash generates a 128-bit hash from a fixed length message or file.
This is done in two phases:

1. hash calculation
2. size injection

### Parameters and mode

imohash takes two parameters, as well as the message length:

* sample size (s)
* sampling threshold (t)
* message length (L)

There are two mode of operation: **sampled** and **full**. Mode is
determined as follows:

```
if (s > 0) && (t > 0) && (L > t) && (t > 2s)
  mode = sampled
else
  mode = full
```

### Hash calculation

The core hashing routine uses [MurmurHash3](https://code.google.com/p/smhasher/wiki/MurmurHash3) in a 128-bit configuration.
Hashing in *Full* mode is identical to passing the entire
message to Murmhash3.  *Sampled* mode constructs a new message using
three samples from the original:

Message M of length L is an array of bytes, M[0]...M[L-1]. If
L > t, full mode is used and h'=Murmur3(M). Otherwise, samples are selected and concatenated as follows:

```
middle = floor(L/2)
S0 = M[0:s-1]           // samples are s bytes long
S1 = M[middle:middle+s]
S2 = M[L-s:L-1]

h' = Murmur3(concat(S0, S1, S2))
```
### Size injection

Size is inserted into the hash directly. This means that two files
that differ in size are guaranteed to have different hashes.

The message size is converted to a variable-length integer (varint)
using 128-bit encoding. Consult [Google Protobuf documentation](https://developers.google.com/protocol-buffers/docs/encoding#varints) for more
information on the technique.

The result of encoding will be an array **v** of 1 or more bytes. This
array will replace the highest-order bytes of h.

```
h = concat(v, h'[len(v):])
```

h is the final imosum hash.

## Default parameters

The default imohash parameters are:

s = 16384
t = 131072

t was chosen to delay sampling until file size was outside the range
of "small" files, such as text files that might be hand-edited and
escape both size changes and being detect by sampling. s was chosen to
provide a large enough sample to distiguish files of like size, but
still small enough to provide high performance.

An application should adjust these values as necessary.

## Test Vectors

(Note: these have not been independently verified using another implementation.)

To avoid offset errors in testing, the test messages need to not repeat
trivially. To this end, MD5 is used to generate pseudorandom test data, 16 bytes at a time,
By repeatedly updating the hash with 'A'. M(n) shall be a test data n bytes long:

```
M(n):
   msg = []
   while len(msg) < n:
       Md5.Write('A')
       msg = msg + Md5.Sum()
   return msg[0:n]

// M(16)      ==     7fc56270e7a70fa81a5935b72eacbe29
// M(1000000) == ... 197c74f51423765786516442fd1c9832
```

Test vectors for imohash of M length n using sample size s and sample
threshold t.

```
  s       t     M(n)                 I
{16384, 131072, 0,      "00000000000000000000000000000000"},
{16384, 131072, 1,      "01659e2ec0f3c75bf39e43a41adb5d4f"},
{16384, 131072, 127,    "7f47671cc79d4374404b807249f3166e"},
{16384, 131072, 128,    "800183e5dbea2e5199ef7c8ea963a463"},
{16384, 131072, 4095,   "ff1f770d90d3773949d89880efa17e60"},
{16384, 131072, 4096,   "802048c26d66de432dbfc71afca6705d"},
{16384, 131072, 131072, "8080085a3d3af2cb4b3a957811cdf370"},
{16384, 131073, 131072, "808008282d3f3b53e1fd132cc51fcc1d"},
{16384, 131072, 500000, "a0c21e44a0ba3bddee802a9d1c5332ca"},
{50,    131072, 300000, "e0a712edd8815c606344aed13c44adcf"},
```




