package siec

import (
	"math/big"
)

func (curve *SIEC255Params) affineToProjective(x, y *big.Int) (X, Y, Z *big.Int) {
	X, Y, Z = new(big.Int), new(big.Int), new(big.Int)
	X.Set(x)
	X.Mod(X, curve.P)
	Y.Set(y)
	Y.Mod(Y, curve.P)
	Z.SetInt64(1)
	return
}

func (curve *SIEC255Params) projectiveToAffine(X, Y, Z *big.Int) (x, y *big.Int) {
	x, y = new(big.Int), new(big.Int)
	if Z.Sign() == 0 {
		return new(big.Int), new(big.Int)
	}
	Zinv := new(big.Int).ModInverse(Z, curve.P)
	Zinvsq := new(big.Int).Mul(Zinv, Zinv)
	x.Mul(X, Zinvsq)
	x.Mod(x, curve.P)
	y.Mul(Y, Zinvsq.Mul(Zinvsq, Zinv))
	y.Mod(y, curve.P)
	return
}

// assumes Z1 = Z2 = 1.
func (curve *SIEC255Params) mmadd2007bl(X1, Y1, X2, Y2 *big.Int) (X3, Y3, Z3 *big.Int) {
	w := new(big.Int)
	ww := new(big.Int)
	// H = X2-X1
	H := new(big.Int).Sub(X2, X1)
	// HH = H^2
	HH := new(big.Int).Mul(H, H)
	// I = 4*HH
	I := new(big.Int).Lsh(HH, 2)
	// J = H*I
	J := new(big.Int).Mul(H, I)
	// r = 2*(Y2-Y1)
	w.Sub(Y2, Y1)
	r := new(big.Int).Lsh(w, 1)
	// V = X1*I
	V := new(big.Int).Mul(X1, I)
	// X3 = r^2-J-2*V
	w.Mul(r, r)
	ww.Add(J, ww.Lsh(V, 1))
	X3 = new(big.Int).Sub(w, ww)
	// Y3 = r*(V-X3)-2*Y1*J
	w.Mul(r, w.Sub(V, X3))
	ww.Lsh(ww.Mul(Y1, J), 1)
	Y3 = new(big.Int).Sub(w, ww)
	// Z3 = 2*H
	Z3 = new(big.Int).Lsh(H, 1)
	return
}

// http://hyperelliptic.org/EFD/g1p/auto-shortw-jacobian-0.html#addition-add-2001-b
func (curve *SIEC255Params) add2007bl(X1, Y1, Z1, X2, Y2, Z2 *big.Int) (X3, Y3, Z3 *big.Int) {
	if X1.BitLen() == 0 && Y1.BitLen() == 0 && Z1.BitLen() == 0 {
		return X2, Y2, Z2
	}
	if X2.BitLen() == 0 && Y2.BitLen() == 0 && Z2.BitLen() == 0 {
		return X1, Y1, Z1
	}
	w := new(big.Int)
	ww := new(big.Int)
	// Z1Z1 = Z1^2
	Z1Z1 := new(big.Int).Mul(Z1, Z1)
	// Z2Z2 = Z2^2
	Z2Z2 := new(big.Int).Mul(Z2, Z2)
	// U1 = X1*Z2Z2
	U1 := new(big.Int).Mul(X1, Z2Z2)
	// U2 = X2*Z1Z1
	U2 := new(big.Int).Mul(X2, Z1Z1)
	// S1 = Y1*Z2*Z2Z2
	w.Mul(Z2, Z2Z2)
	S1 := new(big.Int).Mul(Y1, w)
	// S2 = Y2*Z1*Z1Z1
	w.Mul(Z1, Z1Z1)
	S2 := new(big.Int).Mul(Y2, w)
	// H = U2-U1
	H := new(big.Int).Sub(U2, U1)
	// I = (2*H)^2
	ww.Lsh(H, 1)
	I := new(big.Int).Mul(ww, ww)
	// J = H*I
	J := new(big.Int).Mul(H, I)
	// r = 2*(S2-S1)
	w.Sub(S2, S1)
	r := new(big.Int).Lsh(w, 1)
	// V = U1*I
	V := new(big.Int).Mul(U1, I)
	// X3 = r^2-J-2*V
	w.Mul(r, r)
	ww.Lsh(V, 1)
	ww.Add(J, ww)
	X3 = new(big.Int).Sub(w, ww)
	X3.Mod(X3, curve.P)
	// Y3 = r*(V-X3)-2*S1*J
	w.Sub(V, X3)
	w.Mul(r, w)
	ww.Mul(S1, J)
	ww.Lsh(ww, 1)
	Y3 = new(big.Int).Sub(w, ww)
	Y3.Mod(Y3, curve.P)
	// Z3 = ((Z1+Z2)^2-Z1Z1-Z2Z2)*H
	w.Add(Z1, Z2)
	w.Mul(w, w)
	ww.Add(Z1Z1, Z2Z2)
	w.Sub(w, ww)
	Z3 = new(big.Int).Mul(w, H)
	Z3.Mod(Z3, curve.P)
	return
}

// http://hyperelliptic.org/EFD/g1p/auto-shortw-jacobian-0.html#doubling-dbl-2009-l
func (curve *SIEC255Params) dbl2009l(X1, Y1, Z1 *big.Int) (X3, Y3, Z3 *big.Int) {
	w := new(big.Int)
	m := new(big.Int)
	// A = X1^2
	A := new(big.Int).Mul(X1, X1)
	A.Mod(A, curve.P)
	// B = Y1^2
	B := new(big.Int).Mul(Y1, Y1)
	B.Mod(B, curve.P)
	// C = B^2
	C := new(big.Int).Mul(B, B)
	C.Mod(C, curve.P)
	// D = 2*((X1+B)^2-A-C)
	w.Add(X1, B)
	D := new(big.Int).Lsh(w.Sub(w.Mul(w, w), m.Add(A, C)), 1)
	D.Mod(D, curve.P)
	// E = 3*A
	E := new(big.Int).Mul(three, A)
	E.Mod(E, curve.P)
	// F = E^2
	F := new(big.Int).Mul(E, E)
	F.Mod(F, curve.P)
	// X3 = F-2*D
	X3 = new(big.Int).Sub(F, w.Lsh(D, 1))
	X3.Mod(X3, curve.P)
	// Y3 = E*(D-X3)-8*C
	Y3 = new(big.Int).Sub(w.Mul(E, w.Sub(D, X3)), m.Mul(eight, C))
	Y3.Mod(Y3, curve.P)
	// Z3 = 2*Y1*Z1
	Z3 = w.Lsh(w.Mul(Z1, Y1), 1)
	Z3.Mod(Z3, curve.P)
	return
}

func (curve *SIEC255Params) projectiveScalarMult(Bx, By *big.Int, k []byte) (*big.Int, *big.Int) {
	Bz := new(big.Int).SetInt64(1)
	x, y, z := new(big.Int), new(big.Int), new(big.Int)
	for _, byte := range k {
		for bitNum := 0; bitNum < 8; bitNum++ {
			x, y, z = curve.dbl2009l(x, y, z)
			if byte&0x80 == 0x80 {
				x, y, z = curve.add2007bl(Bx, By, Bz, x, y, z)
			}
			byte <<= 1
		}
	}
	return curve.projectiveToAffine(x, y, z)
}

func (curve *SIEC255Params) projectiveScalarBaseMult(k []byte) (*big.Int, *big.Int) {
	return curve.projectiveScalarMult(curve.Gx, curve.Gy, k)
}
