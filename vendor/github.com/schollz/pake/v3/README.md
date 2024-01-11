# pake

[![travis](https://travis-ci.org/schollz/pake.svg?branch=master)](https://travis-ci.org/schollz/pake) 
[![go report card](https://goreportcard.com/badge/github.com/schollz/pake)](https://goreportcard.com/report/github.com/schollz/pake)
[![Coverage Status](https://coveralls.io/repos/github/schollz/pake/badge.svg)](https://coveralls.io/github/schollz/pake)
[![godocs](https://godoc.org/github.com/schollz/pake?status.svg)](https://godoc.org/github.com/schollz/pake) 

This library will help you allow two parties to generate a mutual secret key by using a weak key that is known to both beforehand (e.g. via some other channel of communication). This is a simple API for an implementation of password-authenticated key exchange (PAKE). This protocol is derived from [Dan Boneh and Victor Shoup's cryptography book](https://crypto.stanford.edu/~dabo/cryptobook/BonehShoup_0_4.pdf) (pg 789, "PAKE2 protocol). I decided to create this library so I could use PAKE in my file-transfer utility, [croc](https://github.com/schollz/croc).


## Install

```
go get -u github.com/schollz/pake/v3
```

## Usage 

![Explanation of algorithm](https://i.imgur.com/s7oQWVP.png)

```golang
// both parties should have a weak key
weakKey := []byte{1, 2, 3}

// initialize A
A, err := pake.InitCurve(weakKey, 0, "siec")
if err != nil {
    panic(err)
}
// initialize B
B, err := pake.InitCurve(weakKey, 1, "siec")
if err != nil {
    panic(err)
}

// send A's stuff to B
err = B.Update(A.Bytes())
if err != nil {
    panic(err)
}

// send B's stuff to A
err = A.Update(B.Bytes())
if err != nil {
    panic(err)
}

// both P and Q now have strong key generated from weak key
kA, _ := A.SessionKey()
kB, _ := A.SessionKey()
fmt.Println(bytes.Equal(kA, kB))
// Output: true
```

When passing *P* and *Q* back and forth, the structure is being marshalled using `Bytes()`, which prevents any private variables from being accessed from either party.

Each function has an error. The error become non-nil when some part of the algorithm fails verification: i.e. the points are not along the elliptic curve, or if a hash from either party is not identified. If this happens, you should abort and start a new PAKE transfer as it would have been compromised. 

## Hard-coded elliptic curve points

The elliptic curve points are hard-coded to prevent an application from allowing users to supply their own points (which could be backdoors by choosing points with known discrete logs). Public points can be verified [via sage](https://sagecell.sagemath.org/?z=eJzNVk1v3MgRvQvQfyDkw85gJaWrqr9qkQ1AckgjyMXB5mCsYQvNZnc8yFhSZsa7Ehb-73mULNv5wCKLXSDhYdhDVlVX1Xuvmmm3u8rv9z-UQ_Nt89OH05PTk2fNd38c-tOTP13-fnv4-_4of8CrP79P8z4dt3nclt28upD16cntFi_4DXFovsad3cON-OHm8bui5qJ5jLH-HcMB9t9_v7rdXl7f7N-t1utluwEPh91ue4vg_ZLJ6vm4ul2fvzLnpK_XzbNm-Ka5f8Mwu3sjiEp6evJ8cVq9cufErx-ipE91vDo7bEs-e71YLG-WghzTnk5PvsMzc7cxOsRoDCv1XXSivu99oCAqHG3btmbTetu1j_maO0Pj__xCgf8vuYAZ02MuxpE6GTr1FAfqtaVRWVum1nQ-OmuGoeVNG9h1qp2QG6WLnY2qsB8JMJDzpKKOhj4MKqEj77g33Ua6jrrRBHFBNmOMsuFe7EjDaB2NG-s7Z2Q0Bky4e8ylx45xML4Lxho7aL_RQYa-856xQWcta-9tJFHjZOzAiDFybEcPF7uRTde2ZDs3hDCMQ3DKcRxo04PcLY9jGzeDiI2d9BSdbxGtGzUMYRDqeXDdxnvkcv-IEUVBI3wborbS9cZY18fWjZ3lPmyo26jG0Vlr1QVFaj5SaMduQ4GDDMi4R-ghsKpDweyt6Z0znRqSsd2Y4Emc9MFE33Lgnq2JsRvUBq_jhrx36Mv1b8WX1lH0MUTpRh7Vo8Fj3xuycQxGW7cxwMr12kXg2tvQDl3nxy7QoCQRqevPijydT4uAv5TviwuA81m_z5oXF-z8oxqJ0DE2UZmMOM82Bs9eA3qoVq0J4IsTg86QFUuO1QhZ0NSJeCFBozzSDovsHSbC_r-NCSzUIwyzRfODN2LZMrNT48mAe8TGWvGo9vDQ-Wx1Fr_sV8BIFZ-8D7EQCDizn0IkraEokMoP9qHUKQE71uimgm0lT8a5HNxsYWhyiXO0SbMjsmnKqQqF4KMhY2sy85MqXcpe3BxTkqRSwiRlmmaHlBW5TNk7mo2fTM5OJlO9TLlIlsIBaRk7LYp6COQnCjNVLsQZtYYaYemL85KsNZVDMBLmSDyXSSSZahNqs9haYwb7Fzk8BLK1oFmFa6EUqk6xlFAmm0I2VQt5XjJYejphRJRc8jSBNj5KmKqj6pqfJdCFPDLo44nw_O78-f2_MwoE-mdGSbSPjELKhg1AVI-lBROIOSiGCBmwXKC1aJwJivOBgaSHbj2gVBtZBaOGrQsMKMgqZGIElLOB_QI9u6jwNhAQYc4gBBHLcn7tf9XOHjMMlp6DdxifClIxQPdYRwVAJoL5DPKCqnAC9UH3ZY7IEzsTGq7sQRLSxCGBExAAOBoC4I14kueUg3xip85UvHMVo6AarrXaOOe5FIgLhJqnaBA9E3CbTU5k5lp8zQ71KQ6ChElXInpVkqsQVvWFIooJOQcoKsoTe8Ek4ppCYVlYYsHZYpyfitTIM-HgoFmzL7VE7AzNQd1odHUYjTEs3Ef5CqUkLjCfOUWXY5lnKZlTqZ_YnVIMOTFPJUJmAmVOlAOVKmzSHCwSnGXyHKcUdIImoVZLxQBTy0ki5jFXdjMoitngsyRnXUEOwMSkKXxiP2AMc4FwMVM4--pmLRqnigOCZ-ikxmppnmaOmsiGXHQGKCSTq2aK2ZjkDVJDOgE16xySFayzmpJMcfVXSwNK-PJjafvu9mZ_bN6mw9vddlqezKU2dXs9X93ebK-Pq-H8UMr87XR2tv7m9KTB9RLeeHNZ9zfvrqb7YzmsPrpfHt4mWi3268t5-9dyOK7W52e77fG4K2frR-8f3253pfnL_n35GG65jvv7L_4t174c3--vm-Fyt63Hq7vVcDmlQ7mqD5-j69XL9fry7n61_uxU7nK5Pf5LlJfN1xj4S1XLv9OTerNv_lbuzwcU0Hzuy-X2WN4dVk8F3u6XwusZLJevZNw-nDcvluV_6MtXeX-T-av1h6cCf7k3PXj_AxzM6dk=&lang=sage&interacts=eJyLjgUAARUAuQ==) using hashes of `croc1` and `croc2`:

```python
all_curves = {}

# SIEC
K.<isqrt3> = QuadraticField(-3)
pi = 2^127 + 2^25 + 2^12 + 2^6 + (1 - isqrt3)/2
p = ZZ(pi.norm())

E = EllipticCurve(GF(p),[0,19]) # E: y^2 = x^3 + 19
G = E([5,12])

all_curves["siec"] = E


# 521r1
S = 0xD09E8800291CB85396CC6717393284AAA0DA64BA
p = 0x01FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF
a = 0x01FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFC
b = 0x0051953EB9618E1C9A1F929A21A0B68540EEA2DA725B99B315F3B8B489918EF109E156193951EC7E937B1652C0BD3BB1BF073573DF883D2C34F1EF451FD46B503F00
Gx= 0x00C6858E06B70404E9CD9E3ECB662395B4429C648139053FB521F828AF606B4D3DBAA14B5E77EFE75928FE1DC127A2FFA8DE3348B3C1856A429BF97E7E31C2E5BD66
Gy= 0x011839296A789A3BC0045C8A5FB42C7D1BD998F54449579B446817AFBD17273E662C97EE72995EF42640C550B9013FAD0761353C7086A272C24088BE94769FD16650
n = 0x01FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFA51868783BF2F966B7FCC0148F709A5D03BB5C9B8899C47AEBB6FB71E91386409

E = EllipticCurve(GF(p),[a,b])
all_curves["P-521"] = E

# P-256
p = 115792089210356248762697446949407573530086143415290314195533631308867097853951
r = 115792089210356248762697446949407573529996955224135760342422259061068512044369
s = 0xc49d360886e704936a6678e1139d26b7819f7e90
c = 0x7efba1662985be9403cb055c75d4f7e0ce8d84a9c5114abcaf3177680104fa0d
b = 0x5ac635d8aa3a93e7b3ebbd55769886bc651d06b0cc53b0f63bce3c3e27d2604b
Gx = 0x6b17d1f2e12c4247f8bce6e563a440f277037d812deb33a0f4a13945d898c296
Gy = 0x4fe342e2fe1a7f9b8ee7eb4a7c0f9e162bce33576b315ececbb6406837bf51f5 

E = EllipticCurve(GF(p),[-3,b])
G = E([Gx,Gy])
all_curves["P-256"] = E

# P-384
p = 39402006196394479212279040100143613805079739270465446667948293404245721771496870329047266088258938001861606973112319
r = 39402006196394479212279040100143613805079739270465446667946905279627659399113263569398956308152294913554433653942643
s = 0xa335926aa319a27a1d00896a6773a4827acdac73
c = 0x79d1e655f868f02fff48dcdee14151ddb80643c1406d0ca10dfe6fc52009540a495e8042ea5f744f6e184667cc722483
b = 0xb3312fa7e23ee7e4988e056be3f82d19181d9c6efe8141120314088f5013875ac656398d8a2ed19d2a85c8edd3ec2aef
Gx = 0xaa87ca22be8b05378eb1c71ef320ad746e1d3b628ba79b9859f741e082542a385502f25dbf55296c3a545e3872760ab7
Gy = 0x3617de4a96262c6f5d9e98bf9292dc29f8f41dbd289a147ce9da3113b5f0b8c00a60b1ce1d7e819d7a431d7c90ea0e5f
E = EllipticCurve(GF(p),[-3,b])
G = E([Gx,Gy])
all_curves["P-384"] = E


import hashlib

def find_point(E,seed=b""):
    X = int.from_bytes(hashlib.sha1(seed).digest(),"little")
    while True:
        try:
            return E.lift_x(E.base_field()(X)).xy()
        except:
            X += 1

    
for key,E in all_curves.items():
    print(f"key = {key}, P = {find_point(E,seed=b'croc2')}")
    print(f"key = {key}, P = {find_point(E,seed=b'croc1')}")
```

which returns

```
key = siec, P = (793136080485469241208656611513609866400481671853, 18458907634222644275952014841865282643645472623913459400556233196838128612339)
key = siec, P = (1086685267857089638167386722555472967068468061489, 19593504966619549205903364028255899745298716108914514072669075231742699650911)
key = P-521, P = (793136080485469241208656611513609866400481671852, 4032821203812196944795502391345776760852202059010382256134592838722123385325802540879231526503456158741518531456199762365161310489884151533417829496019094620)
key = P-521, P = (1086685267857089638167386722555472967068468061489, 5010916268086655347194655708160715195931018676225831839835602465999566066450501167246678404591906342753230577187831311039273858772817427392089150297708931207)
key = P-256, P = (793136080485469241208656611513609866400481671852, 59748757929350367369315811184980635230185250460108398961713395032485227207304)
key = P-256, P = (1086685267857089638167386722555472967068468061489, 9157340230202296554417312816309453883742349874205386245733062928888341584123)
key = P-384, P = (793136080485469241208656611513609866400481671852, 7854890799382392388170852325516804266858248936799429260403044177981810983054351714387874260245230531084533936948596)
key = P-384, P = (1086685267857089638167386722555472967068468061489, 21898206562669911998235297167979083576432197282633635629145270958059347586763418294901448537278960988843108277491616)
```

which are the points used [in the code](https://github.com/schollz/pake/blob/master/pake.go#L76-L107).

## Contributing

Pull requests are welcome. Feel free to...

- Revise documentation
- Add new features
- Fix bugs
- Suggest improvements

## Thanks

Thanks [@tscholl2](https://github.com/tscholl2) for lots of implementation help, fixes, and developing the novel ["siec" curve](https://doi.org/10.1080/10586458.2017.1412371).


## License

MIT
