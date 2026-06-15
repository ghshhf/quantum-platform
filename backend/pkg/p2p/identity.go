package p2p

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Identity 节点身份：私钥 + 派生的 Node ID
type Identity struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
	ID         string // Node ID = PublicKey 前 16 字节 hex
	Name       string // 显示名
}

// LoadOrCreateIdentity 加载已有身份，或生成新的
func LoadOrCreateIdentity(storageDir, name string) (*Identity, error) {
	if err := os.MkdirAll(storageDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir storage: %w", err)
	}
	keyFile := filepath.Join(storageDir, "identity.key")

	// 尝试加载
	if b, err := os.ReadFile(keyFile); err == nil && len(b) == ed25519.PrivateKeySize {
		priv := ed25519.PrivateKey(b)
		pub := priv.Public().(ed25519.PublicKey)
		return &Identity{
			PrivateKey: priv,
			PublicKey:  pub,
			ID:         nodeIDFromPubKey(pub),
			Name:       name,
		}, nil
	}

	// 生成新的
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	if err := os.WriteFile(keyFile, []byte(priv), 0o600); err != nil {
		return nil, fmt.Errorf("save key: %w", err)
	}
	return &Identity{
		PrivateKey: priv,
		PublicKey:  pub,
		ID:         nodeIDFromPubKey(pub),
		Name:       name,
	}, nil
}

// nodeIDFromPubKey 取公钥 SHA256 的前 16 字节作为 Node ID（32 hex 字符）
func nodeIDFromPubKey(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:16])
}

// Sign 为消息签名：sig = Ed25519(From || Type || Payload)
func (id *Identity) Sign(m *Message) error {
	buf := make([]byte, 0, len(id.ID)+1+len(m.Payload))
	buf = append(buf, []byte(m.From)...)
	buf = append(buf, byte(m.Type))
	buf = append(buf, m.Payload...)
	m.Signature = ed25519.Sign(id.PrivateKey, buf)
	return nil
}

// VerifySignature 验证消息签名（用已知公钥）
func VerifySignature(m *Message, pub ed25519.PublicKey) bool {
	buf := make([]byte, 0, len(m.From)+1+len(m.Payload))
	buf = append(buf, []byte(m.From)...)
	buf = append(buf, byte(m.Type))
	buf = append(buf, m.Payload...)
	return ed25519.Verify(pub, buf, m.Signature)
}

// EncodeMessage 把消息序列化为 JSON + 长度前缀，用于 TCP
func EncodeMessage(m *Message) ([]byte, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	// 4 字节大端长度前缀
	out := make([]byte, 4+len(b))
	out[0] = byte(len(b) >> 24)
	out[1] = byte(len(b) >> 16)
	out[2] = byte(len(b) >> 8)
	out[3] = byte(len(b))
	copy(out[4:], b)
	return out, nil
}

// DecodeMessage 反序列化一条消息（仅 payload 部分）
func DecodeMessage(b []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
