# Mobilvpn — Sunucu Tarafı (Linux / Ubuntu)

Bu dizin, `app/` altındaki Aura VPN Android istemcisinin karşı tarafını oluşturan,
**gerçek** bir VPN tünel sunucusu ve onu yöneten REST API'dir. İstemci zaten bir
"ince istemci" (thin client) olarak tasarlanmış: tüm tünelleme, IP atama, şifreleme
anahtarı üretimi ve telemetri hesaplaması sunucu tarafında yapılır. Bu dizin o
sunucu tarafını, Ubuntu 22.04/24.04 üzerinde çalışacak şekilde, **hız** ve
**gizlilik** önceliğiyle uygular.

## Mimari özeti

```
Android app (app/)
      │  HTTPS  (backendUrl, kullanıcı tarafından ayarlanır)
      ▼
vpn-api (Go, bağımlılıksız, TLS'i kendi sonlandırır)
      │
      ├─ WireGuard'ı yönetir  → `wg` / `ip` (çekirdek WireGuard arayüzü: wg0)
      ├─ OpenVPN'i yönetir    → easy-rsa (sertifika) + management soketi (UDP:1194, TCP:443)
      ├─ strongSwan'ı yönetir → swanctl conf.d (oturum başına IKEv2, isteğe bağlı)
      └─ unbound'u yönetir    → DNS-over-TLS ile sızıntısız, loglamasız çözümleme
```

- **WireGuard** — birincil/önerilen protokol: en hızlı el sıkışma, en küçük saldırı
  yüzeyi, ChaCha20-Poly1305.
- **OpenVPN (UDP + TCP/443)** — ikincil protokol; TCP/443 özellikle kısıtlayıcı
  güvenlik duvarlarını aşmak (DPI/sansür direnci) için var.
- **IKEv2/IPsec** — isteğe bağlı (`install.sh --with-ikev2`): telefonlar
  Wi-Fi ve hücresel arasında dolaşırken tüneli koparmayan MOBIKE için ve
  işletim sisteminin yerleşik IKEv2 istemcisini kullanmak isteyenler için.

Tam teknik tasarım için **docs/ARCHITECTURE.md**, uç nokta sözleşmesi için
**docs/API.md**, tehdit modeli / no-logs politikası için **docs/SECURITY.md**
dosyalarına bakın.

## Dizin yapısı

```
server/
  api/            Go modülü (vpn-api) — yönetim API'si ve tünel orkestrasyon mantığı
  scripts/        Ubuntu kurulum/sertleştirme betikleri (00-70, numaralı, sırayla çalışır)
                  + verify.sh (dağıtım doktoru) ve test-peer.sh (gerçek tünel testi)
  systemd/        Kalıcı olarak /etc/systemd/system altına kopyalanan servis birimleri
  nft/            nftables güvenlik duvarı şablonu
  config/         config.json ve nodes.json şablonları/örnekleri
  docs/           Mimari, API ve güvenlik dokümantasyonu
  install.sh      Tüm betikleri sırayla çalıştıran orkestratör + kurulum sihirbazı
  bootstrap.sh    Tek satırlık kurulum: depoyu GitHub'dan çekip install.sh'ı çalıştırır
```

## Hızlı kurulum (tek satır)

Taze bir Ubuntu 22.04/24.04 sunucuya SSH ile bağlanın ve şunu yapıştırın —
depoyu GitHub'dan kendisi çeker, gerekli paketleri kurar ve **kurulum
sihirbazını** başlatır:

```bash
curl -fsSL https://raw.githubusercontent.com/agamert37-cmd/Mobilvpn/main/server/bootstrap.sh | sudo bash
```

Sihirbaz sırayla şunları sorar (hepsinin makul bir varsayılanı vardır, Enter
geçer):

| Soru | Not |
|---|---|
| Alan adı | Android uygulamasının bağlanacağı ad; sunucunun genel IP'si (otomatik tespit edilip gösterilir) buna işaret etmeli. Boş bırakılırsa TLS'siz, yalnızca test amaçlı bir kurulum önerilir |
| Bölge etiketi | Uygulamada `clusterRegion` olarak görünür |
| Düğüm kimliği | Filo içinde bu sunucuyu ayırt eder |
| SSH portu | `sshd` yapılandırmasından **otomatik okunur**; güvenlik duvarı yalnızca bunu açık bırakır |
| IKEv2/IPsec | İsteğe bağlı ikinci protokol (varsayılan: hayır) |

Sonunda bir özet gösterilir ve onay istenir; onaylamazsanız hiçbir değişiklik
yapılmadan çıkar.

> Sorular `/dev/tty`'den okunur, stdin'den değil — `curl | sudo bash` içinde
> stdin betiğin kendisidir. Bu yüzden tek satırlık kurulum da tam etkileşimlidir.

Yukarıdaki adres `main` dalını kullanır. `server/` henüz `main`'e
birleştirilmediyse (ya da bir özellik dalını denemek istiyorsanız) URL'deki
dal adını değiştirin ve aynı dalı `VPN_REPO_BRANCH` ile verin:

```bash
BRANCH=claude/linux-vpn-tunnel-server-ozhg41
curl -fsSL "https://raw.githubusercontent.com/agamert37-cmd/Mobilvpn/$BRANCH/server/bootstrap.sh" \
  | sudo VPN_REPO_BRANCH="$BRANCH" bash
```

### Sorusuz (otomatik) kurulum

CI, imaj üretimi ya da toplu dağıtım için sihirbazı tamamen atlayın:

```bash
curl -fsSL https://raw.githubusercontent.com/agamert37-cmd/Mobilvpn/main/server/bootstrap.sh \
  | sudo bash -s -- --domain vpn.example.com --yes --with-ikev2
```

`-s --`'den sonraki her şey `server/install.sh`'a olduğu gibi aktarılır.
`bootstrap.sh` kendisi de üç ortam değişkeni tanır:

| Değişken | Varsayılan | Anlamı |
|---|---|---|
| `VPN_REPO_URL` | `https://github.com/agamert37-cmd/Mobilvpn.git` | Klonlanacak depo |
| `VPN_REPO_BRANCH` | `main` | Kurulacak dal |
| `VPN_SRC_DIR` | `/opt/mobilvpn-src` | Kaynak kodun tutulacağı yer (ikinci çalıştırmada güncellenir) |

### Depo elinizdeyse

```bash
cd server
sudo VPN_NODE_REGION="İstanbul, TR" ./install.sh --domain vpn.example.com
```

Argümansız çalıştırırsanız yine sihirbaz açılır. `--yes` sorusuz ilerler,
`--skip-tls` / `--skip-firewall` ilgili adımı atlar, `--with-ikev2` IKEv2'yi
ekler.

`--domain`, Android uygulamasının bağlanacağı ve TLS sertifikasının (Let's
Encrypt / certbot) alınacağı genel ana bilgisayar adıdır — DNS'te bu sunucunun
genel IP'sine işaret etmelidir. Kurulum betiği sırayla:

1. Paketleri kurar (wireguard-tools, openvpn, easy-rsa, unbound, nftables, golang-go, ...)
   ve `--with-ikev2` verildiyse strongSwan'ı kurup kendi CA'sını üretir
2. Çekirdek ağ ayarlarını uygular (BBR, ip_forward, tampon boyutları)
3. WireGuard sunucu kimliğini üretir ve `wg0`'ı ayağa kaldırır
4. easy-rsa PKI'sini kurar ve iki OpenVPN sunucusunu (UDP/1194, TCP/443) ayağa kaldırır
5. unbound'u DNS-over-TLS ileten, loglamayan bir çözümleyici olarak yapılandırır
6. Let's Encrypt sertifikası alır (certbot, standalone HTTP-01)
7. `vpn-api` Go ikili dosyasını derler ve systemd servisi olarak kurar
8. nftables güvenlik duvarını uygular (**bu adım en sona bırakılmıştır** — bir
   hata SSH erişiminizi kesebileceği için, önceki adımların çalıştığını
   doğruladıktan sonra uygulanır)

Kurulum bittiğinde Android uygulamasındaki **Sunucu Mimarisi** ekranından
"Sunucu API Uç Noktası" alanına `https://vpn.example.com:8443/` yazmanız
yeterlidir (bkz. `app/src/main/java/com/example/ui/components/BackendServerDialog.kt`).

### Önemli ortam değişkenleri

Her betik, makul varsayılanlarla çalışır ama şunları özelleştirebilirsiniz
(`install.sh`'tan önce `export` edin ya da satır başında geçin):

| Değişken | Varsayılan | Anlamı |
|---|---|---|
| `VPN_API_DOMAIN` | *(zorunlu)* | Genel ana bilgisayar adı; TLS sertifikası ve `publicEndpointHost` için |
| `VPN_NODE_ID` | `hostname` | Bu düğümün kimliği (filodaki diğer düğümlerden ayırt etmek için) |
| `VPN_NODE_REGION` | `Unconfigured Region` | `/api/v1/health` içindeki `clusterRegion` |
| `VPN_SSH_PORT` | `22` | Güvenlik duvarının SSH için açık bırakacağı port — **gerçek SSH portunuzla eşleşmeli** |
| `VPN_API_PUBLIC_PORT` | `8443` | vpn-api'nin genel HTTPS portu (443, OpenVPN-TCP tarafından kullanıldığı için boş bırakılır) |
| `VPN_WG_PORT` / `VPN_WG_SUBNET` | `51820` / `10.66.0.0/16` | WireGuard dinleme portu ve tünel alt ağı |
| `VPN_OVPN_UDP_PORT` / `VPN_OVPN_TCP_PORT` | `1194` / `443` | OpenVPN portları |
| `VPN_WAN_IFACE` | otomatik algılanır | NAT/masquerade için WAN arayüzü |
| `VPN_WG_MTU` | otomatik algılanır | İstemciye bildirilen tünel MTU'su. Canlı `wg0` arayüzünden okunur (wg-quick onu "WAN MTU - 80" yapar); **sabit 1420 varsaymayın**, WAN MTU'su 1500 olmayan bulutlarda yanlıştır |
| `VPN_UNBOUND_THREADS` | `nproc` (en fazla 8) | unbound iş parçacığı sayısı; slab sayısı buna göre 2'nin kuvvetine yuvarlanır |
| `VPN_ENABLE_IPV6` | otomatik algılanır | Çift yığın tünel. Ana bilgisayarda global IPv6 varsa açılır (IPv6 sızıntısını kapatır); yoksa hiç açılmaz. `1`/`0` ile zorlanabilir |
| `VPN_WG_SUBNET_V6` | `fd00:66::/64` | Tünelin IPv6 (ULA) öneki |
| `VPN_ENABLE_IKEV2` | `0` | IKEv2/IPsec kurulumu (`install.sh --with-ikev2` ile aynı) |
| `VPN_IKEV2_SUBNET` | `10.88.0.0/16` | IKEv2 istemcilerinin tünel alt ağı |
| `VPN_IKEV2_LOGLEVEL` | `-1` (sessiz) | charon günlük seviyesi. `-1` dışındaki her değer IKE_SA kurulurken **eşin gerçek IP'sini** journal'a yazar; yalnızca sorun ayıklarken geçici olarak yükseltin |

Betikler ayrıca tek tek de çalıştırılabilir (`scripts/00-prereqs.sh`, ...),
tümü idempotenttir (ikinci çalıştırma güvenlidir).

## Doğrulama ve sorun giderme

Kurulumdan sonra (ya da bir şeyler bozulduğunda) iki araç var:

```bash
sudo /opt/mobilvpn/scripts/verify.sh
```

Canlı kurulumu uçtan uca denetler ve **ilk hatada durmaz** — bulduğu her
sorunu düzeltme ipucuyla birlikte listeler: servis durumları, `ip_forward`/BBR,
WireGuard anahtar tutarlılığı (arayüzün gerçekten kullandığı anahtar ile
istemcilere dağıtılan anahtar aynı mı — ayrışırsa hiçbir el sıkışma çalışmaz),
dosya izinleri, **OpenVPN CRL süresi** (dolarsa OpenVPN tüm istemcileri
reddeder, gözden kaçması kolay bir arıza), nftables NAT/izolasyon/DNS
kuralları, unbound'un açık çözümleyici olup olmadığı, API sağlığı ve TLS
sertifika ömrü. Kritik bir hata varsa çıkış kodu 1'dir.

```bash
sudo /opt/mobilvpn/scripts/test-peer.sh
```

Gerçek bir tek kullanımlık WireGuard peer'ı **üretim API yolundan geçerek**
oluşturur (IPAM, `wg set`, connect yanıt sözleşmesi — hepsi gerçek) ve
taranabilir bir QR kod ile istemci yapılandırması basar. Böylece tüneli
resmî WireGuard uygulamasıyla, Android istemcisinden bağımsız olarak
doğrulayabilirsiniz. Özel anahtar sunucuya hiç gönderilmez (bring-your-own-key
yolu). Test bitince: `sudo ./test-peer.sh --disconnect <sessionId>`.

Ek olarak `vpn-api -print-config`, config.json'ı gerçek ayrıştırıcısıyla
okuyup etkin değerleri kabuk değişkenleri olarak basar (yukarıdaki iki betik
de bunu kullanır, böylece bash içinde JSON ayrıştırmaya gerek kalmaz).

## Geliştirme / test

`api/` bağımlılıksız (yalnızca Go standart kütüphanesi) bir Go modülüdür:

```bash
cd server/api
go build ./...
go vet ./...
go test ./... -race
```

Yerel geliştirme için `config.json` olmadan da çalışır (`config.Default()`
kullanılır, WireGuard/OpenVPN araçları yoksa nazikçe "UNAVAILABLE" döner):

```bash
go run ./cmd/vpn-api
curl http://127.0.0.1:8080/api/v1/health
```

## Kapsam ve bilinen sınırlamalar

- Bu görev yalnızca **sunucu tarafını** kapsar; `app/` altındaki Android istemcisine
  hiçbir değişiklik yapılmamıştır (mevcut DTO sözleşmesiyle tam uyumludur, üzerine
  yalnızca istemcinin görmezden geldiği ek/opsiyonel alanlar eklenmiştir — bkz.
  docs/API.md).
- IKEv2 varsayılan olarak kapalıdır (`--with-ikev2` ile açılır); açıkken her
  oturum kendi strongSwan bağlantısını, kendi tek adresli havuzunu ve kendi
  EAP kimlik bilgisini alır.
- IPv6 tüneli, ana bilgisayarın global IPv6 bağlantısı varsa otomatik olarak
  açılır ve IPv6 sızıntısını kapatır; yoksa hiçbir yerde `::/0` reklam
  edilmez ve sızıntı istemci tarafında çözülmelidir (bkz. docs/SECURITY.md).
- Çok düğümlü (fleet) mimari uygulanmıştır ama gerçek coğrafi dağıtım/DNS
  tabanlı yönlendirme operatörün kendi sorumluluğundadır; bu depo tek bir
  düğümü sıfır yapılandırmayla, birden fazla düğümü `config/nodes.example.json`
  ile destekler.
