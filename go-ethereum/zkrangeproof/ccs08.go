// Copyright 2017 ING Bank N.V.
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package zkrangeproof

import (
	"strconv"
	"bytes"
	"crypto/sha256"
	"math/big"
	"crypto/rand"
	"github.com/ing-bank/zkrangeproof/go-ethereum/crypto/bn256"
	"github.com/ing-bank/zkrangeproof/go-ethereum/byteconversion"
	"fmt"
)

//Constants that we are going to be used frequently, then we just need to compute them once.
var (
	G1 = new(bn256.G1).ScalarBaseMult(new(big.Int).SetInt64(1))
	G2 = new(bn256.G2).ScalarBaseMult(new(big.Int).SetInt64(1))
	E = bn256.Pair(G1, G2)
)

/*
params contains elements generated by the verifier, which are necessary for the prover.
This must be computed in a trusted setup.
*/
type params struct {
	signatures map[string]*bn256.G2
	H *bn256.G2
	// must protect the private key
	kp keypair
	u, l int64
}

/*
proof contains the necessary elements for the ZK proof.
*/
type proof struct {
	V []*bn256.G2
	D,C *bn256.G2
	a []*bn256.GT
	s,t,zsig,zv []*big.Int
	c,m,zr *big.Int
}

/*
Commit method corresponds to the Pedersen commitment scheme. Namely, given input 
message x, and randomness r, it outputs g^x.h^r.
*/
func Commit(x,r *big.Int, p params) (*bn256.G2, error) {
	var (
		C *bn256.G2
	)
	C = new(bn256.G2).ScalarBaseMult(x)
	C.Add(C, new(bn256.G2).ScalarMult(p.H, r))
	return C, nil
}

/*
Hash is responsible for the computing a Zp element given an element from G2 and GT.
*/
func Hash(a []*bn256.GT, D *bn256.G2) (*big.Int, error) {
	digest := sha256.New()
	for i := range a {
		digest.Write([]byte(a[i].String()))
	}
	digest.Write([]byte(D.String()))
	output := digest.Sum(nil)
	tmp := output[0: len(output)]
	return byteconversion.FromByteArray(tmp)
}

/* 
Decompose receives as input a bigint x and outputs an array of integers such that
x = sum(xi.u^i), i.e. it returns the decomposition of x into base u.
*/
func Decompose(x *big.Int, u int64, l int64) ([]int64, error) {
	var (
		result []int64
		i int64
	)
	result = make([]int64, l, l)
	i = 0
	for i<l {
		result[i] = Mod(x, new(big.Int).SetInt64(u)).Int64()
		x = new(big.Int).Div(x, new(big.Int).SetInt64(u))
		i = i + 1
	}
	return result, nil
}

/*
Setup generates the signature for the interval [0,u^l).
The value of u should be roughly b/log(b), but we can choose smaller values in
order to get smaller parameters, at the cost of have worse performance.
*/
func Setup(u, l int64) (params, error) {
	var (
		i int64
		p params
	)
	p.kp, _ = keygen()

	p.signatures = make(map[string]*bn256.G2)
	i = 0
	for i < u {
		sig_i, _ := sign(new(big.Int).SetInt64(i), p.kp.privk)
		p.signatures[strconv.FormatInt(i, 10)] = sig_i 
		i = i + 1
	}
	//TODO: protect the master key
	h := GetBigInt("18560948149108576432482904553159745978835170526553990798435819795989606410925")
	p.H = new(bn256.G2).ScalarBaseMult(h)
	p.u = u
	p.l = l
	return p, nil
}

/*
Prove method is used to produce the ZKRP proof.
*/
func Prove(x,r *big.Int, p params) (proof, error) {
	var (
		i int64
		v []*big.Int
		A *bn256.G2
		proof_out proof
	)
	decx, _ := Decompose(x, p.u, p.l)	
	fmt.Println(decx)

	v = make([]*big.Int, p.l, p.l)
	proof_out.V  = make([]*bn256.G2, p.l, p.l)
	proof_out.a  = make([]*bn256.GT, p.l, p.l)
	proof_out.s = make([]*big.Int, p.l, p.l)
	proof_out.t = make([]*big.Int, p.l, p.l)
	proof_out.zsig = make([]*big.Int, p.l, p.l)
	proof_out.zv = make([]*big.Int, p.l, p.l)
	proof_out.D = new(bn256.G2) 
	proof_out.D.SetInfinity()
	proof_out.m, _ = rand.Int(rand.Reader, bn256.Order)
	D := new(bn256.G2).ScalarMult(p.H, proof_out.m)
	for i = 0; i< p.l; i++ {
		v[i], _ = rand.Int(rand.Reader, bn256.Order)
		//TODO: must verify if x belongs to p.signatures
		A = p.signatures[strconv.FormatInt(decx[i], 10)]
		proof_out.V[i] = new(bn256.G2).ScalarMult(A, v[i])
		proof_out.s[i], _ = rand.Int(rand.Reader, bn256.Order)
		proof_out.t[i], _ = rand.Int(rand.Reader, bn256.Order)
		proof_out.a[i] = bn256.Pair(G1, proof_out.V[i])
		proof_out.a[i].ScalarMult(proof_out.a[i], proof_out.s[i])
		proof_out.a[i].Invert(proof_out.a[i])
		proof_out.a[i].Add(proof_out.a[i], new(bn256.GT).ScalarMult(E, proof_out.t[i]))
	

		ui := new(big.Int).Exp(new(big.Int).SetInt64(p.u), new(big.Int).SetInt64(i), nil)
		muisi := new(big.Int).Mul(proof_out.s[i], ui)
		muisi = Mod(muisi, bn256.Order)
		aux := new(bn256.G2).ScalarBaseMult(muisi)
		D.Add(D, aux)
	}	
	proof_out.D.Add(proof_out.D, D)
	
	proof_out.C, _ = Commit(x, r, p)
	proof_out.c, _ = Hash(proof_out.a, proof_out.D)
	proof_out.c = Mod(proof_out.c, bn256.Order)
	//proof_out.c = new(big.Int).SetInt64(0)

	proof_out.zr = Sub(proof_out.m, Multiply(r, proof_out.c))
	proof_out.zr = Mod(proof_out.zr, bn256.Order)
	for i = 0; i< p.l; i++ {
		proof_out.zsig[i] = Sub(proof_out.s[i], Multiply(new(big.Int).SetInt64(decx[i]), proof_out.c))
		proof_out.zsig[i] = Mod(proof_out.zsig[i], bn256.Order)
		proof_out.zv[i] = Sub(proof_out.t[i], Multiply(v[i], proof_out.c))
		proof_out.zv[i] = Mod(proof_out.zv[i], bn256.Order)
	}
	return proof_out, nil
}

/*
Verify is used to validate the ZKRP proof. It return true iff the proof is valid.
*/
func Verify(proof_out *proof, p *params, pubk *bn256.G1) (bool, error) {
	var (
		i int64
		D *bn256.G2
		r1, r2 bool
		p1,p2 *bn256.GT
	)
	// D == C^c.h^ zr.g^zsig ?
	D = new(bn256.G2).ScalarMult(proof_out.C, proof_out.c)
	D.Add(D, new(bn256.G2).ScalarMult(p.H, proof_out.zr)) 	
	for i = 0; i< p.l; i++ {
		ui := new(big.Int).Exp(new(big.Int).SetInt64(p.u), new(big.Int).SetInt64(i), nil)
		muizsigi := new(big.Int).Mul(proof_out.zsig[i], ui)
		muizsigi = Mod(muizsigi, bn256.Order)
		aux := new(bn256.G2).ScalarBaseMult(muizsigi)
		D.Add(D, aux) 	
	}

	DBytes := D.Marshal()
	pDBytes := proof_out.D.Marshal()
	r1 = bytes.Equal(DBytes, pDBytes)

	r2 = true
	for i = 0; i< p.l; i++ {
		// a == [e(V,y)^c].[e(V,g)^-zsig].[e(g,g)^zv]
		// TODO: avoid using many variables
		p1 = bn256.Pair(pubk, proof_out.V[i])
		p1.ScalarMult(p1, proof_out.c)
		p2 = bn256.Pair(G1, proof_out.V[i])
		p2.ScalarMult(p2, proof_out.zsig[i])
		p2.Invert(p2)
		p1.Add(p1, p2)
		p1.Add(p1, new(bn256.GT).ScalarMult(E, proof_out.zv[i]))
	
		pBytes := p1.Marshal()
		aBytes := proof_out.a[i].Marshal()

		r2 = r2 && bytes.Equal(pBytes, aBytes) 
	}
	return r1 && r2, nil
}

