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

### MTU: neden sabit bir sayı değil

`config.json` içindeki `wireguard.mtu`, Android istemcisinin kendi TUN
arayüzüne uyguladığı değerdir. wg-quick ise sunucu tarafındaki arayüzü
"varsayılan rotanın MTU'su − 80" ile açar (`/usr/bin/wg-quick`,
`set_mtu_up`).

İkisi ayrışırsa istemci, sunucunun arayüzüne ve yola sığmayan paketler
üretir; sonuç parçalanma ya da DF ayarlıysa sessiz düşmedir — kullanıcıya
"bağlanıyor ama yavaş/takılıyor" diye yansır.

Sabit `1420` yalnızca WAN MTU'su 1500 olan makinelerde doğrudur. Bu
kolayca gözden kaçar çünkü 1500 − 80 = 1420. Oysa GCP 1460, pek çok
bulut/overlay ağı 1400–1450, PPPoE 1492 kullanır; bu depo geliştirilirken
kullanılan makinenin WAN MTU'su da 1400'dü, yani doğru değer 1320'ydi.

Bu yüzden `60-vpn-api-service.sh` değeri canlı arayüzden okur (yer
gerçeği), arayüz henüz yoksa wg-quick'in formülünü tekrarlar ve IPv6'nın
zorunlu asgarisi olan 1280'in altına inmez. `verify.sh` ikisinin
uyuştuğunu ayrıca denetler.

### Sertifika algoritması: 3 saniyelik bütçe bir doğruluk sınırıdır

Android istemcisinin OkHttp zaman aşımı 3 saniye ve aşıldığında istisnayı
yakalayıp **simüle edilmiş** bir bağlantı üretiyor
(`data/VpnRepository.kt`, `requestConnect`'in catch bloğu). Yani yavaş bir
`/connect`, hata olarak değil, "bağlandınız" olarak görünür — kullanıcı
korumasızken korunduğunu sanır. Bu, bir performans meselesi değil,
güvenlik meselesidir.

`/connect` yolundaki en pahalı iş OpenVPN sertifikası üretmek ve bu iş
sıraya alınmak zorunda: easy-rsa, `pki/index.txt` ve `pki/serial`
dosyalarını kilitsiz yazar (bkz. bir sonraki bölüm). Sıraya alınmış
üretim ölçüldü:

| algoritma | sertifika başına | 8 eşzamanlı bağlantı |
|---|---|---|
| RSA-2048 | ~0,40 sn | 2,42 sn (3 sn bütçesinin sınırında) |
| EC P-256 | ~0,03 sn | **0,21 sn** |

RSA ile sekiz kişinin aynı anda bağlanması bütçeyi zorlar; biraz daha
kalabalıkta sonuncu kullanıcı sessizce simülasyona düşer. Bu yüzden yeni
PKI'ler EC P-256 ile kuruluyor. Gerçek openvpn 2.6.19 ile uçtan uca
doğrulandı: `TLSv1.3`, `ECprime256v1 / ecdsa-with-SHA256`,
`Initialization Sequence Completed`.

Yan fayda: EC'de `dh` parametresi gerekmez (anahtar değişimi ECDHE ile
yapılır), böylece kurulumdaki dakikalar süren `gen-dh` adımı de ortadan
kalkar. Mevcut RSA kurulumları olduğu gibi çalışmaya devam eder; betik
CA anahtarından algoritmayı okuyup `dh` yönergesini ona göre yazar.

### easy-rsa eşzamanlılığı: bozulan CA veritabanı

easy-rsa, `openssl ca` etrafında bir kabuk sarmalayıcısıdır ve durumunu
iki paylaşılan dosyada tutar: `pki/index.txt` (sertifika veritabanı) ve
`pki/serial`. Hiçbiri kilit altında yazılmaz.

Tek bir PKI'ye karşı 8 sertifika paralel üretildiğinde ölçülen sonuç:
**iki sertifika aynı seri numarasını aldı** ve **8 sertifikadan yalnızca
7'si index.txt'ye yazıldı**.

İkisi de iptali (revocation) bozar ve iptal, `/disconnect`'in bir OpenVPN
oturumunu gerçekten sonlandırma yoludur:

- aynı seriyi paylaşan iki sertifikadan birini iptal etmek diğerini de
  iptal eder (CRL serileri listeler), yani ilgisiz bir kullanıcı atılır;
- index.txt'de kaydı olmayan bir sertifika **hiç iptal edilemez**, çünkü
  `easyrsa revoke` onu orada arar — CA süresi dolana kadar geçerli kalan
  bir kimlik bilgisi.

İki kullanıcının aynı anda bağlan'a basması olağan trafiktir, uç durum
değil. Bu yüzden `internal/openvpn/pki.go` her mutasyonu hem süreç içi bir
mutex hem de PKI dizini üzerinde bir `flock` ile sıraya alır; dosya kilidi,
`scripts/test-peer.sh` ya da operatörün elle çalıştırdığı bir `easyrsa`
komutunun canlı bir `/connect` ile iç içe geçmesini engeller.
`revoke` ve `gen-crl` tek bir kritik bölümdür: yarım yazılmış bir
index.txt'den üretilen CRL, öldürmesi gereken sertifikayı sessizce atlar.

### unbound: ölçülmüş iş parçacığı ayarı

Tünelin tüm DNS'i bu çözümleyiciden geçtiği için gecikmesi doğrudan
hissedilir. unbound varsayılanı tek iş parçacığıdır.

Ölçüm (unbound 1.19.2, 4 vCPU, 4 süreçli boru hatlı yük, 3 tekrarın
medyanı, tamamı önbellekten yanıtlanan sorgular):

| yapılandırma | yanıt/s | taban |
|---|---|---|
| varsayılan (1 iş parçacığı) | 144.924 | — |
| `num-threads=4` + eşleşen slab'ler | 244.403 | **1.69x** |

Üç tekrarın üçünde de tutarlı (236k–251k). `so-reuseport` bu tezgâhta
belirgin fark göstermedi (gürültülü) ama çok iş parçacıklı unbound için
standart pratiktir. Önbellek boyutlarının etkisi bu ölçümle **test
edilemedi** — tezgâh yalnızca 50 farklı ada soruyor — ama varsayılan 4m,
çok kullanıcılı bir çözümleyici için küçük olduğundan RAM'e göre
ölçekleniyor.

İlk (yanıltıcı) ölçüm iki yapılandırma arasında hiç fark göstermemişti;
sebebi sunucu değil, tezgâhın kendisiydi: GIL altındaki 16 Python
iş parçacığı eşzamanlı `send`/`recv` yaparken darboğaz istemcideydi.
Ayrı süreçlere ve boru hattına geçilince fark ortaya çıktı.

## Gizlilik için yapılan seçimler

Ayrıntılı tehdit modeli ve no-logs politikası için **docs/SECURITY.md**'ye
bakın; özetle: istemci izolasyonu (varsayılan-reddet forward zinciri altında
tünel-alt-ağından-tünel-alt-ağına kural yok), zorunlu DNS-over-TLS (nftables
tünel arayüzlerinden 53 numaralı portu, kendi çözümleyicimiz dışında engeller),
hiçbir katmanda trafik/bağlantı içeriği loglaması, ve oturum durumunun
diskte yalnızca en asgari, kimlik-doğrulama-dışı alanlarla tutulması.
