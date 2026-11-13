package producer

import (
	"strings"
	"testing"

	"github.com/yesvichu/sub2surge/internal/model"
)

func TestSurgeVMess(t *testing.T) {
	proxy := model.Proxy{
		Name:      "HK,01",
		Type:      "vmess",
		Server:    "hk.example.com",
		Port:      443,
		UUID:      "uuid",
		Cipher:    "auto",
		Network:   "ws",
		WSPath:    "/ws",
		WSHeaders: map[string]string{"Host": "cdn.example.com"},
		TLS:       true,
		SNI:       "cdn.example.com",
		ALPN:      []string{"h2", "http/1.1"},
		UDP:       true,
	}
	got, err := Surge(proxy)
	if err != nil {
		t.Fatalf("Surge: %v", err)
	}
	want := `HK01=vmess,hk.example.com,443,username=uuid,ws=true,ws-path=/ws,ws-headers="Host:cdn.example.com",vmess-aead=true,tls=true,sni="cdn.example.com",alpn="h2,http/1.1",udp-relay=true`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSurgeVMessEncryptMethod(t *testing.T) {
	proxy := model.Proxy{
		Name: "v", Type: "vmess", Server: "a.com", Port: 443, UUID: "u",
		Cipher: "chacha20-poly1305", AlterID: 2,
	}
	got, err := Surge(proxy)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "encrypt-method=chacha20-ietf-poly1305") {
		t.Fatalf("missing encrypt-method: %s", got)
	}
	if !strings.Contains(got, "vmess-aead=false") {
		t.Fatalf("expected aead=false for alterId=2: %s", got)
	}
}

func TestSurgeShadowsocksObfs(t *testing.T) {
	proxy := model.Proxy{
		Name:     "SS",
		Type:     "ss",
		Server:   "ss.example.com",
		Port:     8388,
		Cipher:   "aes-256-gcm",
		Password: "p@ss\"word",
		Plugin:   "obfs-local",
		PluginOpts: map[string]string{
			"obfs":      "http",
			"obfs-host": "bing.com",
		},
		TFO: true,
	}
	got, err := Surge(proxy)
	if err != nil {
		t.Fatalf("Surge: %v", err)
	}
	want := `SS=ss,ss.example.com,8388,encrypt-method=aes-256-gcm,password="p@ss'word",obfs=http,obfs-host=bing.com,tfo=true`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSurgeHysteria2AndTUIC(t *testing.T) {
	hy := model.Proxy{
		Name: "HY", Type: "hysteria2", Server: "hy.example.com", Port: 443,
		Password: "pass", Obfs: "salamander", ObfsPassword: "obfs",
		Ports: "1000-2000,3000", HopInterval: "30", SNI: "hy.example.com", Insecure: true,
	}
	got, err := Surge(hy)
	if err != nil {
		t.Fatal(err)
	}
	want := `HY=hysteria2,hy.example.com,443,password="pass",port-hopping="1000-2000;3000",port-hopping-interval=30,salamander-password="obfs",sni="hy.example.com",skip-cert-verify=true`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}

	tuic := model.Proxy{
		Name: "TU", Type: "tuic", Server: "tuic.example.com", Port: 443,
		UUID: "uuid", Password: "pass", ALPN: []string{"h3"},
	}
	got, err = Surge(tuic)
	if err != nil {
		t.Fatal(err)
	}
	want = `TU=tuic-v5,tuic.example.com,443,uuid=uuid,password="pass",alpn="h3"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}

	tuic.Token = "token"
	got, err = Surge(tuic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "TU=tuic,") || !strings.Contains(got, "token=token") {
		t.Fatalf("unexpected tuic token output: %s", got)
	}
}

func TestSurgeRejectsUnsupported(t *testing.T) {
	cases := []model.Proxy{
		{Name: "v", Type: "vless", Server: "a.com", Port: 443, UUID: "u", Reality: true},
		{Name: "v", Type: "vless", Server: "a.com", Port: 443, UUID: "u", Reality: true, RealityPublicKey: "pbk", SNI: "s", Network: "ws"},
		{Name: "v", Type: "vless", Server: "a.com", Port: 443, UUID: "u", Network: "httpupgrade"},
		{Name: "v", Type: "vmess", Server: "a.com", Port: 443, UUID: "u", Reality: true},
		{Name: "v", Type: "vmess", Server: "a.com", Port: 443, UUID: "u", Network: "grpc"},
		{Name: "s", Type: "ss", Server: "a.com", Port: 443, Cipher: "aes-256-gcm", Password: "p", Plugin: "v2ray-plugin"},
	}
	for _, proxy := range cases {
		if _, err := Surge(proxy); err == nil {
			t.Fatalf("expected error for %+v", proxy)
		}
	}
}

func TestSurgeSocks5TLS(t *testing.T) {
	proxy := model.Proxy{
		Name: "S5", Type: "socks5", Server: "1.2.3.4", Port: 1080,
		Username: "user", Password: "pass", TLS: true, SNI: "s5.example.com", UDP: true,
	}
	got, err := Surge(proxy)
	if err != nil {
		t.Fatal(err)
	}
	want := `S5=socks5-tls,1.2.3.4,1080,username="user",password="pass",sni="s5.example.com",udp-relay=true`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSurgeVLESS(t *testing.T) {
	ws := model.Proxy{
		Name: "VL", Type: "vless", Server: "v.example.com", Port: 443, UUID: "uuid",
		Network: "ws", WSPath: "/vless", WSHeaders: map[string]string{"Host": "cdn.example.com"},
		TLS: true, SNI: "cdn.example.com", ALPN: []string{"h2"}, UDP: true,
	}
	got, err := Surge(ws)
	if err != nil {
		t.Fatal(err)
	}
	want := `VL=vless,v.example.com,443,username=uuid,ws=true,ws-path=/vless,ws-headers="Host:cdn.example.com",tls=true,sni="cdn.example.com",alpn="h2",udp-relay=true`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}

	reality := model.Proxy{
		Name: "VL", Type: "vless", Server: "1.2.3.4", Port: 443, UUID: "uuid",
		Network: "tcp", TLS: true, Reality: true, RealityPublicKey: "pbk", RealityShortID: "sid",
		SNI: "www.microsoft.com", Flow: "xtls-rprx-vision",
	}
	got, err = Surge(reality)
	if err != nil {
		t.Fatal(err)
	}
	want = `VL=vless,1.2.3.4,443,username=uuid,flow=xtls-rprx-vision,reality-public-key=pbk,reality-short-id=sid,sni="www.microsoft.com"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}

	grpc := model.Proxy{
		Name: "VL", Type: "vless", Server: "g.example.com", Port: 443, UUID: "uuid",
		Network: "grpc", GRPCServiceName: "mygrpc", TLS: true, SNI: "g.example.com",
	}
	got, err = Surge(grpc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "grpc=true,grpc-service-name=mygrpc,tls=true") {
		t.Fatalf("unexpected grpc line: %s", got)
	}

	// Vision is invalid on WS; drop it instead of emitting an invalid policy.
	ws.Flow = "xtls-rprx-vision"
	got, err = Surge(ws)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "flow=") {
		t.Fatalf("flow should be omitted on ws: %s", got)
	}
}

func TestSurgeTrojanHasNoTLSFlag(t *testing.T) {
	proxy := model.Proxy{
		Name: "T", Type: "trojan", Server: "t.example.com", Port: 443, Password: "p",
		Network: "ws", WSPath: "/ws", WSHeaders: map[string]string{"Host": "cdn.example.com"},
		TLS: true, SNI: "t.example.com", Insecure: true,
	}
	got, err := Surge(proxy)
	if err != nil {
		t.Fatal(err)
	}
	want := `T=trojan,t.example.com,443,password="p",ws=true,ws-path=/ws,ws-headers="Host:cdn.example.com",sni="t.example.com",skip-cert-verify=true`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSurgeNameCannotStartComment(t *testing.T) {
	proxy := model.Proxy{Name: "#1", Type: "vmess", Server: "a.com", Port: 443, UUID: "u"}
	got, err := Surge(proxy)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "_#1=vmess,") {
		t.Fatalf("name should be prefixed: %s", got)
	}
}
