# Mimari

## Bileşenler

```
                         ┌─────────────────────────────────────────┐
                         │              vpn-api (Go)                │
                         │  net/http + stdlib crypto/tls, üçüncü     │
                         │  taraf bağımlılığı yok                    │
                         │                                           │
  Android app  ──HTTPS──▶│  httpapi (router/handlers)                │
                         │     │        │            │               │
                         │     ▼        ▼            ▼               │
                         │  wireguard  openvpn      fleet             │
                         │  (wg/ip)    (easyrsa/    (nodes.json +     │
                         │             mgmt socket)  health polling)  │
                         │     │        │                             │
                         │  session (bellek-içi + state.json)         │
                         │  ipam (sanal IP havuzu)                    │
                         │  ratelimit (token bucket)                  │
                         └──────┬─────────┬─────────────────────────┘
                                │         │
                          wg0 (çekirdek) tun-udp/tun-tcp (OpenVPN)
                                │         │
                            nftables (NAT + izolasyon + DNS zorlaması)
                                │
                            unbound (DoT ileten, loglamayan çözümleyici)
```

`api/internal/` altındaki paketler:

- **config** — `config.json`'ı yükler; dosya yoksa geliştirme için güvenli
  varsayılanlar döner.
- **session** — canlı tünel oturumlarını bellekte tutar (RWMutex korumalı map);
  yalnızca API süreç yeniden başlatıldığında canlı wg peer'larını/OpenVPN
  sertifikalarını "öksüz" bırakmamak için gereken asgari bilgiyi (genel anahtar/
  ortak ad, sanal IP, zaman damgaları — **trafik verisi değil**) `state.json`'a
  yazar.
- **ipam** — bir CIDR'den sanal IP tahsis eden basit, thread-safe bir havuz.
- **wireguard** — sunucu kimliğini (`crypto/ecdh` ile X25519, `wg genkey` ile
  bit-bit uyumlu) üretir/yükler; peer ekleme/çıkarma/telemetri için `wg`/`ip`
  komutlarını çalıştırır.
- **ikev2** — strongSwan'ın swanctl arayüzünü sürer: oturum başına bir
  bağlantı + tek adresli havuz + EAP kimlik bilgisini `conf.d` altına yazar,
  `swanctl --load-all` ile uygular, telemetriyi `--list-sas --raw`
  çıktısından okur.
- **openvpn** — easy-rsa PKI'sine oturum başına sertifika bastırır/iptal eder;
  OpenVPN'in management soketi (parola korumalı TCP) üzerinden canlı istemci
  istatistiklerini okur ve oturumları zorla sonlandırır.
- **fleet** — bu düğümün ve (varsa) kardeş düğümlerin listesini tutar; kardeş
  düğümlerin `/internal/v1/status` uç noktasını periyodik olarak yoklar (canlı
  yük/sağlık), ve `serverId` bu düğüme ait değilse connect/disconnect/telemetry
  isteklerini o düğüme şeffafça iletir (proxy).
- **ratelimit** — `/api/v1/connect` gibi pahalı uç noktalar için IP başına
  token-bucket sınırlayıcı.
- **httpapi** — yukarıdakileri birleştiren HTTP katmanı: DTO'lar, handler'lar,
  ara katmanlar (panic recovery, loglama, rate limit, iç sır doğrulaması),
  TLS sertifika sıcak yeniden yükleme.

## Neden bağımlılıksız (yalnızca stdlib) Go?

- **Denetlenebilirlik/gizlilik**: üçüncü taraf bağımlılığı yok = tedarik
  zinciri riski yok, izlenecek CVE listesi yok.
- **Çevrimdışı derlenebilirlik**: taze bir Ubuntu kutusunda `go mod download`
  gerektirmez; `apt install golang-go` yeterlidir.
- WireGuard anahtar üretimi bile üçüncü taraf kriptografi gerektirmez:
  `crypto/ecdh` (Go 1.20+) X25519'u native olarak sağlar. Bu depoda gerçek
  `wg pubkey` çıktısıyla bit-bit karşılaştıran bir entegrasyon testi var
  (`internal/wireguard/keys_test.go`).

## Oturum kimliği ve çok-düğümlü (fleet) yönlendirme

Bir oturum kimliği `sess_<nodeId>_<24 hex>` biçimindedir. Bu, **paylaşılan
hiçbir durum olmadan**, hangi düğümün bir oturuma sahip olduğunu bulmayı
sağlar: `/api/v1/disconnect` ya da `/api/v1/telemetry` çağrıldığında, önek bu
düğümün kendi kimliğiyle eşleşmiyorsa istek otomatik olarak doğru kardeş
düğüme (varsa `internalApiUrl`'i yapılandırılmışsa) iletilir. Android
uygulaması tek bir `backendUrl` ile konuşmaya devam eder; hangi düğümün
gerçekte tüneli sağladığı şeffaftır.

Tek düğümlü bir kurulumda `config/nodes.json` yoktur ve `fleet.Load` sadece
kendisini içeren tek elemanlı bir liste döner — sıfır yapılandırma gerekir.

## Protokol tasarım kararları

- **WireGuard, önerilen/varsayılan protokol.** Sunucu, istemcinin ortak
  anahtarını göndermediği (mevcut Android DTO'sunda böyle bir alan yok)
  durumlarda **sunucu tarafında** bir anahtar çifti üretir ("ephemeral" mod)
  ve özel anahtarı yalnızca bir kez, TLS üzerinden, `connect` yanıtında
  döner. İstemci güncellenip `clientPublicKey` göndermeye başladığında,
  özel anahtar cihazdan hiç çıkmaz (tercih edilen yol). Bkz. docs/API.md.
- **OpenVPN, kısıtlı ağlar için ikincil protokol.** Bağlantı anında sunucu
  tarafı henüz "ESTABLISHED" değildir (istemci henüz TLS el sıkışmasını
  başlatmamıştır) — dürüstçe `status: "PROVISIONED"` döner.
- **IKEv2, mobil dolaşım için isteğe bağlı üçüncü protokol.** OpenVPN gibi
  bağlantı anında `PROVISIONED` döner. Her oturum kendi bağlantısını ve
  `eap_id`'sini aldığı için bir oturumun kimlik bilgisi başka bir oturumun
  bağlantısında kullanılamaz; tek adresli havuz da API'nin baştan bildirdiği
  `virtualIp`'yi gerçek kılar.

  Ölçüm notu: her bağlan/kes işlemi `swanctl --load-all` gerektirir. 150
  eşzamanlı oturum yapılandırmasıyla bu yeniden yükleme ölçüldüğünde 0,23 s
  sürdü — kabul edilebilir, ama O(n) olduğu için çok büyük düğümlerde akılda
  tutulmalı.

  strongSwan'ın kendi PKI'si kullanılır (OpenVPN'in easy-rsa CA'sı değil):
  aksi hâlde OpenVPN'i kapatmak IKEv2'yi de bozardı.

## Hız için yapılan seçimler

- Çekirdek WireGuard (kullanıcı alanı değil) — en düşük paket başı yük.
- BBR + `fq` (sysctl), büyütülmüş soket tamponları, büyütülmüş conntrack
  tablosu (`nf_conntrack_max`).
- `wg set` ile canlı peer yönetimi — arayüz yeniden başlatma/`wg-quick`
  yeniden çalıştırma yok.
- unbound'da `prefetch`/`serve-expired` — DNS gecikmesi algısını azaltır.
- Yönetim API'si tamamen `net/http` + stdlib JSON; harici çatı yükü yok.

## Gizlilik için yapılan seçimler

Ayrıntılı tehdit modeli ve no-logs politikası için **docs/SECURITY.md**'ye
bakın; özetle: istemci izolasyonu (varsayılan-reddet forward zinciri altında
tünel-alt-ağından-tünel-alt-ağına kural yok), zorunlu DNS-over-TLS (nftables
tünel arayüzlerinden 53 numaralı portu, kendi çözümleyicimiz dışında engeller),
hiçbir katmanda trafik/bağlantı içeriği loglaması, ve oturum durumunun
diskte yalnızca en asgari, kimlik-doğrulama-dışı alanlarla tutulması.
