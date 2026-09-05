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
| OpenVPN | `verb 0` — yalnızca çalışma zamanı hataları (ör. port çakışması); `--status` dosyası kaldırıldı | **İstemcinin gerçek IP'si**, tünel IP eşlemesi, ortak ad, bağlantı zamanı, trafik içeriği/hedefleri |
| IKEv2 (charon) | `journal { default = -1 }` — hiçbir şey | IKE_SA kurulurken eşin gerçek IP'si ve kimliği |
| unbound | `verbosity: 0`, `log-queries: no`, `log-replies: no`, `use-syslog: no` | Hiçbir DNS sorgusu |
| vpn-api HTTP erişim logu | metod + yol + durum kodu + süre | **İstemci IP'si asla**, sorgu dizesi asla (bu yüzden `sessionId` taşıyan `GET /telemetry?sessionId=...` bile loglanmaz — `r.URL.Path` sorgu dizesini içermez) |
| vpn-api oturum durumu (`state.json`) | genel anahtar/ortak ad, sanal IP, zaman damgaları (yalnızca süreç yeniden başlatıldığında peer'ları/sertifikaları öksüz bırakmamak için) | Trafik verisi, hedef, gerçek istemci IP'si |

Bu **API-katmanı** loglama kararıdır ve gerekirse operatör kendi
sorumluluğunda `vpn-api`'yi ayrıntılı loglama için değiştirebilir — ama
varsayılan, ürünün "gizlilik" vaadini karşılayacak şekilde en az bilgidir.

### Bu tablo neden ölçümle yazıldı

Yukarıdaki satırlar varsayılan değerlerin "makul göründüğü" için değil,
gerçek süreçlere karşı ölçüldüğü için böyle. İlk sürüm OpenVPN'i `verb 3`
ile kuruyordu ve bu doküman onu "yalnızca bağlantı/hata olayları" diye
tarif ediyordu. Bu **yanlıştı**.

Gerçek bir `openvpn 2.6.19` sunucusuna bir istemci bağlanıp sunucu
günlüğü seviye seviye sayıldığında:

| verb | toplam satır | istemci IP'si içeren satır |
|---|---|---|
| 0 | 0 | 0 |
| 1 | 24 | 12 |
| 2 | 31 | 18 |
| 3 (eski varsayılan) | 42 | 23 |

Çıktı üreten en düşük seviye olan `verb 1`'de bile şu satırlar yazılıyor:

```
203.0.113.9:53352 [client1] Peer Connection Initiated with [AF_INET]203.0.113.9:53352
client1/203.0.113.9:53352 MULTI_sva: pool returned IPv4=10.77.0.2
```

İkinci satır, kullanıcının **gerçek genel IP'sini atanan tünel IP'siyle**
zaman damgalı olarak eşleştirir. Tünel IP'si üzerinden tutulan herhangi
bir kayıtla birleştirildiğinde tam kimliklendirme sağlar — yani bir
no-logs VPN'in tutmaması gereken kaydın tam olarak kendisi.

`verb 0` hiçbir istemci satırı yazmaz ama çalışma zamanı hatalarını
göstermeye devam eder (ölçüldü — port çakışmasında `TCP/UDP: Socket bind
failed on local address ...: Address already in use`), ve birim
`Type=notify` olduğu için systemd'nin hazır-olma tespiti log çıktısına
bağlı değildir.

### `--status` dosyası: Ubuntu biriminin gömdüğü sızıntı

Ubuntu'nun hazır `openvpn-server@.service` birimi ExecStart'a bir durum
dosyası gömer:

```
ExecStart=/usr/sbin/openvpn --status %t/openvpn-server/status-%i.log \
          --status-version 2 --suppress-timestamps --config %i.conf
```

Bağlı bir istemciyle bu dosyanın içeriği ölçüldü:

```
CLIENT_LIST,client1,203.0.113.9:43271,10.77.0.2,,3142,3148,2026-09-05 21:47:14,...
ROUTING_TABLE,10.77.0.2,client1,203.0.113.9:43271,2026-09-05 21:47:14,...
```

Gerçek IP ↔ tünel IP ↔ ortak ad ↔ bağlantı zamanı. `/run` altında olduğu
için yeniden başlatmayı atlatmaz, ama makinenin tüm çalışma süresi
boyunca okunabilir durumdadır.

`scripts/30-openvpn-setup.sh` bu yüzden bir systemd drop-in kurar
(`/etc/systemd/system/openvpn-server@.service.d/no-status.conf`) ve
ExecStart'ı `--status` olmadan yeniden tanımlar.

**Telemetri bundan etkilenmez.** `vpn-api` bu dosyayı hiç okumaz;
management soketine `status 2` gönderir. Gerçek bir daemon'a karşı
doğrulandı: `--status` hiç verilmediğinde bile management soketi bayt
sayaçlarını döndürmeye devam ediyor, dolayısıyla `/api/v1/telemetry`
çalışmaya devam eder.

### charon (IKEv2) günlük seviyesi

strongSwan'ın varsayılan journal seviyesi (`default = 1`) IKE_SA
kurulurken eşin gerçek IP'sini ve kimliğini yazar. `35-ikev2-setup.sh`
bunu `-1`'e (tamamen sessiz) çeker. Ölçüldü (strongSwan 5.9.13, aynı
açılış): `default = 1` → 24 satır, `default = -1` → 0 satır.

Sorun ayıklamak için geçici olarak `VPN_IKEV2_LOGLEVEL=1` ile
kurulabilir; bunun bedeli, o süre boyunca eş IP'lerinin yeniden
loglanmasıdır.

### Denetleme

`scripts/verify.sh` bunların hepsini canlı kurulumda tekrar kontrol eder
(OpenVPN verb seviyesi, drop-in'in varlığı, `/run` altında gerçek IP
içeren bir durum dosyası kalıp kalmadığı, charon seviyesi, unbound'un
sorgu loglaması), böylece bir yükseltme ya da elle düzenleme gizlilik
duruşunu sessizce bozarsa fark edilir.

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
- **IPv6, ana bilgisayarın bağlantısına göre otomatik.** Kurulum betikleri
  gerçek global IPv6 bağlantısı arar (global kapsamlı adres **ve** varsayılan
  rota); varsa tünel çift yığın kurulur: wg0'a bir ULA adresi
  (`fd00:66::1/64`) verilir, IPv6 yönlendirme açılır, nftables NAT66 ve
  IPv6 istemci izolasyonu uygular, unbound v6 üzerinden de yanıt verir ve
  API istemciye `::/0` rotasını bildirir. Bu, klasik **IPv6 sızıntısını**
  kapatır: çift yığın bir istemci artık v6 trafiğini de tünelden geçirir.

  Global IPv6 yoksa hiçbir bileşen v6 açmaz ve API `allowedIps` içinde
  `::/0` **bildirmez** — çünkü taşınamayan bir rotayı reklam etmek
  istemcinin IPv6 trafiğini kara deliğe yollar; sızıntıyı bırakmak bundan
  daha az zararlıdır. Bu durumda sızıntı devam eder ve tam çözümü istemci
  tarafındadır (Android `VpnService.Builder` ile VPN dışı v6'yı engellemek),
  bu deponun kapsamı dışındadır.

  Üç betik (`10-sysctl`, `20-wireguard`, `60-vpn-api-service`) aynı
  `detect_ipv6_support` testini kullanır; ayrışmaları hâlinde "adres var
  ama yönlendirme yok" gibi sessiz arızalar doğardı.
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
- **IKEv2 kimlik bilgileri**: her oturum için rastgele 24 baytlık bir EAP
  parolası üretilir ve yalnızca o oturuma ait strongSwan bağlantısında
  geçerlidir (`eap_id` o oturumun kimliğine sabitlenir) — bir oturumun
  parolası başka bir oturumun bağlantısında çalışmaz. Parola, kök-erişimli
  (0600) tek bir `conf.d` dosyasında durur; bağlantı kesildiğinde önce canlı
  SA sonlandırılır (`swanctl --terminate`), sonra dosya silinip yeniden
  yükleme yapılır, böylece kimlik bilgisi de bağlantı tanımı da ortadan
  kalkar.

  strongSwan kendi CA'sını kullanır; OpenVPN'in easy-rsa CA'sına bağlanmaz,
  aksi hâlde bir protokolü kapatmak diğerini bozardı.

  Kurulum notu: Ubuntu, eski `strongswan-starter` servisini varsayılan olarak
  etkinleştirir ve o da UDP 500/4500'ü bağlar. `35-ikev2-setup.sh` onu devre
  dışı bırakır, `verify.sh` ise tekrar etkinleşmediğini denetler — aksi hâlde
  swanctl tabanlı daemon sessizce başlayamaz.

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
