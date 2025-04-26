package crypto

import "github.com/esrrhs/gohome/crypto/cryptonight"

/*
package crypto

提供了一个加密相关的功能包，主要实现了对 Cryptonight 加密算法的封装。该包定义了 Crypto 结构体，作为 Cryptonight 的接口，通过简单的 API 提供加密和校验相关功能。
*/

type Crypto struct {
	cn *cryptonight.CryptoNight
}

func NewCrypto(family string) *Crypto {
	cy := &Crypto{}
	if family == "" || family == "cryptonight" {
		cy.cn = cryptonight.NewCryptoNight()
	}
	return cy
}

func (c *Crypto) Sum(data []byte, algo string, height uint64) []byte {
	return c.cn.Sum(data, algo, height)
}

func TestSum(algo string) bool {
	return cryptonight.TestSum(algo)
}

func TestAllSum() bool {
	for _, algo := range Algo() {
		if !TestSum(algo) {
			return false
		}
	}
	return true
}

func Algo() []string {
	return cryptonight.Algo()
}
