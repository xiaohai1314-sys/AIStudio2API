// Command sign-release 用 Ed25519 私钥为文件生成 minisign 格式的签名文件。
//
// 私钥与公钥通过环境变量传入：
//
//	MINISIGN_PRIVATE_KEY  64 字节 Ed25519 私钥的 base64（GitHub Secrets 保管）
//	MINISIGN_PUBLIC_KEY   minisign 格式公钥的 base64（算法标识 || 密钥 ID || 公钥，用于签名文件头）
//
// 用法: sign-release <文件>...   在每个文件同目录生成 <文件>.minisig
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign-release:", err)
		os.Exit(1)
	}
}

func run(files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("用法: sign-release <文件>...")
	}
	privRaw, err := base64.StdEncoding.DecodeString(os.Getenv("MINISIGN_PRIVATE_KEY"))
	if err != nil || len(privRaw) != ed25519.PrivateKeySize {
		return fmt.Errorf("MINISIGN_PRIVATE_KEY 缺失或格式错误")
	}
	pubRaw, err := base64.StdEncoding.DecodeString(os.Getenv("MINISIGN_PUBLIC_KEY"))
	if err != nil || len(pubRaw) != 2+8+ed25519.PublicKeySize || string(pubRaw[:2]) != "Ed" {
		return fmt.Errorf("MINISIGN_PUBLIC_KEY 缺失或格式错误")
	}
	priv, keyID := ed25519.PrivateKey(privRaw), pubRaw[2:10]
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("读取 %s: %w", file, err)
		}
		sig := ed25519.Sign(priv, data)
		sigLine := base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), keyID...), sig...))
		trusted := fmt.Sprintf("timestamp:%d\tfile:%s", time.Now().Unix(), filepath.Base(file))
		globalSig := ed25519.Sign(priv, append(sig, trusted...))
		globalLine := base64.StdEncoding.EncodeToString(append(append([]byte("Ed"), keyID...), globalSig...))
		content := fmt.Sprintf("untrusted comment: signature from minisign secret key\n%s\ntrusted comment: %s\n%s\n",
			sigLine, trusted, globalLine)
		if err := os.WriteFile(file+".minisig", []byte(content), 0o644); err != nil {
			return fmt.Errorf("写入 %s.minisig: %w", file, err)
		}
		fmt.Println("已签名:", file+".minisig")
	}
	return nil
}
