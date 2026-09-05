# Güvenlik ve Gizlilik

Bu belge, "gizlilik" önceliğinin somut olarak neye karşılık geldiğini,
neyin **kasıtlı olarak** yapılmadığını ve neden yapılmadığını dürüstçe
açıklar. Abartılı vaatler yerine doğrulanabilir tasarım kararları.

## Tehdit modeli

Bu sunucu, **tek kiracılı** bir VPN çıkış düğümü olarak tasarlandı: kutudaki
tüm süreçler (vpn-api, wg-quick, openvpn-server, unbound) aynı güven
sınırındadır ve hepsi operatör tarafından kontrol edilir. Varsayımlar:

- Tehdit: ağdaki pasif/aktif gözlemciler (ISP, yerel ağ), tünel istemcileri
  arasında sızıntı, ve kutunun kendisine yapılan uzaktan saldırılar.
- Tehdit **değil**: kutuda zaten root/yerel kod çalıştırma yeteneği olan biri
  (bu noktada zaten WireGuard/OpenVPN özel anahtarlarına diskten erişebilir).
- Bu, `vpn-api`'nin **root olarak** çalışma kararını açıklar (aşağıya bakın)
  — çok kiracılı bir sunucuda bu kabul edilemezdi.

## No-logs politikası — katman katman

| Katman | Ne loglanır | Ne loglanmaz |
|---|---|---|
| nftables | (opsiyonel, varsayılan kapalı) hız sınırlı, kaynak/hedefsiz saldırı gürültüsü sayacı | Hiçbir tünel paketinin kaynağı/hedefi/içeriği |
| WireGuard (çekirdek) | Hiçbir şey (kernel WireGuard trafik içeriği ya da hedef loglamaz) | — |
| OpenVPN | `verb 3` — yalnızca bağlantı/hata olayları, systemd unit'in kendi `--status` dosyası (root-only, `/run` altında, kalıcı değil) | Trafik içeriği/hedefleri |
| unbound | `verbosity: 0`, `log-queries: no`, `log-replies: no`, `use-syslog: no` | Hiçbir DNS sorgusu |
| vpn-api HTTP erişim logu | metod + yol + durum kodu + süre | **İstemci IP'si asla**, sorgu dizesi asla (bu yüzden `sessionId` taşıyan `GET /telemetry?sessionId=...` bile loglanmaz — `r.URL.Path` sorgu dizesini içermez) |
| vpn-api oturum durumu (`state.json`) | genel anahtar/ortak ad, sanal IP, zaman damgaları (yalnızca süreç yeniden başlatıldığında peer'ları/sertifikaları öksüz bırakmamak için) | Trafik verisi, hedef, gerçek istemci IP'si |

Bu **API-katmanı** loglama kararıdır ve gerekirse operatör kendi
sorumluluğunda `vpn-api`'yi ayrıntılı loglama için değiştirebilir — ama
varsayılan, ürünün "gizlilik" vaadini karşılayacak şekilde en az bilgidir.

## OpenVPN management arayüzü: neden parola korumalı

Gerçek bir `openvpn 2.6.19` sürecine bağlanıldığında, parolasız bir TCP
management soketi için OpenVPN'in kendisi şu uyarıyı basar:

```
WARNING: Using --management on a TCP port WITHOUT passwords is
STRONGLY discouraged and considered insecure
```

Bu depo bunu ciddiye alır: `scripts/30-openvpn-setup.sh` her kurulumda
rastgele bir parola üretir (`openssl rand -base64 24`,
`/etc/openvpn/server/mgmt.pass`, mod 600) ve hem OpenVPN'in `management`
direktifine hem de `vpn-api`'nin `config.json`'ına (`managementPasswordFile`)
verir. Protokol akışı `internal/openvpn/management.go` içinde, gerçek bir
OpenVPN sürecine karşı doğrulanmış şekilde uygulanmıştır:

```
sunucu:  ENTER PASSWORD:
istemci: <parola>\n
sunucu:  SUCCESS: password is correct
sunucu:  >INFO:OpenVPN Management Interface Version 5 ...
```

## Kabul edilen ödünleşimler (ve nedenleri)

- **vpn-api root olarak çalışır.** `wg`/`ip` komutları CAP_NET_ADMIN ister,
  WireGuard özel anahtarını ve easy-rsa PKI'sini okur/yazar,
  `unbound-control`'ü çağırır — bunların hepsi zaten ayrı ayrı yükseltilmiş
  yetki gerektirir (tıpkı `wg-quick@` ve `openvpn-server@` birimlerinin
  kendilerinin de root olarak başlayıp yalnızca OpenVPN'in veri düzlemi için
  `nobody`'ye düşmesi gibi). `systemd/vpn-api.service`, `ProtectSystem=strict`
  + açık `ReadWritePaths` ile dosya sistemi etki alanını yine de daraltır.
  Daha az ayrıcalıklı, çok-kullanıcılı bir model (CAP_NET_ADMIN'i normal bir
  kullanıcıya `AmbientCapabilities` ile vermek, PKI dizinlerini o kullanıcıya
  devretmek) mümkün ama bu depo kapsamında bir sonraki sertleştirme adımı
  olarak bırakıldı.
- **IPv6 tüneli yok.** `10-sysctl-tuning.sh` varsayılan olarak
  `net.ipv6.conf.all.forwarding=0` bırakır. Bir istemci cihazın **yerel**
  IPv6 bağlantısı varsa (VPN'in dışında), bu IPv6 trafiği tünelin dışından
  gerçek adresle gidebilir — klasik bir "IPv6 sızıntısı". Bunu tam olarak
  kapatmak, çift-yığın (dual-stack) bir tünel + istemci tarafında IPv6'yı
  devre dışı bırakma/kill-switch (Android `VpnService.Builder` seviyesinde,
  bu depronun kapsamındaki sunucu tarafı değil) gerektirir. `VPN_ENABLE_
  IPV6_FORWARDING=1` ile açılabilir ama istemci tarafı desteği eklenene
  kadar önerilmez.
- **OpenVPN management soketleri TCP/127.0.0.1, unix soket değil.** Yalnızca
  loopback'e bağlı, parola korumalı; ama bu makinedeki HERHANGİ bir yerel
  süreç yine de bağlanmayı deneyebilir (dosya sistemi izinleriyle değil, ağ
  erişimiyle sınırlı). Tek-kiracılı tehdit modelimizde kabul edilebilir.
- **`killSwitch`/`autoConnect` sunucu tarafında zorlanamaz.** Bunlar
  temelde istemci cihazın davranışlarıdır; sunucu yalnızca tercih olarak
  saklar. Bkz. docs/API.md.

## Anahtar/sertifika yaşam döngüsü

- **WireGuard sunucu kimliği**: bir kez üretilir (`scripts/20-wireguard-
  setup.sh`), `/etc/wireguard/server_private.key` (mod 600) içinde kalıcı
  olur. `vpn-api`'nin `wireguard.EnsureServerIdentity` fonksiyonu **aynı**
  dosyayı okur — bu iki bileşenin asla farklı anahtarlara sahip olmamasını
  garantiler (aksi halde istemcilere verilen genel anahtar, çekirdek
  arayüzünün gerçekte kullandığı anahtarla uyuşmazdı).
- **WireGuard istemci anahtarları**: "bootstrap modunda" (bkz. API.md)
  sunucu tarafında üretilir ve yalnızca TLS üzerinden, tek seferlik connect
  yanıtında iletilir; diske asla yazılmaz. Bir istemci güncellemesi kendi
  anahtarını üretip yalnızca genel anahtarı gönderdiğinde, özel anahtar
  cihazdan hiç çıkmaz.
- **OpenVPN istemci sertifikaları**: oturum başına, `sessionId` ortak adıyla
  üretilir; `disconnect` çağrıldığında **gerçekten iptal edilir**
  (`easyrsa revoke` + `gen-crl` + yayınlama) — sadece bağlantıyı kesmekle
  kalmaz, sertifikayı kalıcı olarak geçersiz kılar. Bu, verilen `.ovpn`
  profilinin disconnect sonrası yeniden kullanılamamasını sağlar.
- **CRL**: her iptalden sonra yeniden üretilir ve OpenVPN'in okuduğu yola
  kopyalanır; OpenVPN yeni bağlantılarda CRL'i her seferinde yeniden okur —
  servis yeniden başlatma gerekmez.

## Uygulama testleriyle doğrulanan iddialar

Bu belgedeki iddiaların çoğu varsayım değil, bu depodaki testlerle
doğrulanmıştır:

- `internal/wireguard/keys_test.go` → `TestInteropWithRealWgTool`: bu
  kodun ürettiği anahtarların gerçek `wg pubkey` ile bit-bit aynı sonucu
  verdiğini kanıtlar.
- `internal/openvpn/management_test.go` → parola protokolü, gerçek bir
  openvpn 2.6.19 sürecine karşı elle doğrulanan akışın birebir simülasyonu.
- `internal/httpapi/handlers_test.go` ve `fleet_proxy_test.go` → kapasite
  sınırları, IP havuzu serbest bırakma (başarısız `AddPeer` sonrası bile),
  filolar arası proxy yönlendirmesi.

Kurulum betikleri de gerçek `wg`, `easyrsa`, `openvpn`, `unbound`,
`nftables` araçlarına karşı canlı olarak çalıştırılıp doğrulanmıştır
(çekirdek WireGuard arayüz desteği olmayan bir kum havuzunda test
edildiğinden, yalnızca gerçek bir `wg0` arayüzü gerektiren adımlar hariç).

## Çalışan bir kurulumu denetlemek

`scripts/verify.sh`, bu belgedeki güvenlik iddialarının çoğunu **canlı
sistem üzerinde** kontrol eder ve şu üç sessiz arıza sınıfını özellikle
hedefler:

1. **Anahtar ayrışması** — `wg0` arayüzünün gerçekten kullandığı genel
   anahtar ile API'nin istemcilere dağıttığı anahtar farklıysa hiçbir el
   sıkışma tamamlanmaz, ama hiçbir bileşen hata da vermez.
2. **Süresi dolmuş CRL** — easy-rsa'nın varsayılan CRL ömrü 180 gündür;
   dolduğunda OpenVPN *tüm* istemcileri reddeder.
3. **Açık çözümleyici** — unbound yanlışlıkla `0.0.0.0:53` dinlerse hem bir
   DDoS yansıtma aracına dönüşür hem de tünel dışından sorgu kabul eder.

Anahtar/sertifika dosya izinleri (600) de aynı betikte doğrulanır.
