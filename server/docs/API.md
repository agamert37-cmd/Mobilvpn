# API Referansı

Taban URL, Android uygulamasında `BackendServerDialog` üzerinden ayarlanan
`backendUrl`'dir (örn. `https://vpn.example.com:8443/`). Tüm gövdeler JSON'dur.

Alan adları, `app/src/main/java/com/example/network/VpnServerDtos.kt`
içindeki Kotlin veri sınıflarıyla **birebir** eşleşecek şekilde seçilmiştir
(Moshi'nin reflection tabanlı adaptörü, alan adı eşleşmesine dayanır).
Aşağıda "ek alan" olarak işaretlenenler bu sözleşmenin **dışında**dır — Moshi
bunları sessizce yok sayar, mevcut uygulama hiç etkilenmez; yalnızca
güncellenmiş veya gelecekteki bir istemci bunları okuyabilir.

## GET /api/v1/health

```json
{
  "status": "OPERATIONAL",
  "version": "1.0.0",
  "activeNodes": 1,
  "clusterRegion": "İstanbul, TR"
}
```

`activeNodes`, bu düğüm dahil, filodaki sağlıklı düğüm sayısıdır (bkz.
ARCHITECTURE.md § fleet).

## GET /api/v1/servers

`ServerNodeDto` dizisi döner. Tek düğümlü bir kurulumda tek eleman içerir.
`pingMs` ve `loadPercent` için dürüstlük notu: `pingMs`, operatörün
`nodes.json`'da bildirdiği statik bir referans değeridir (sunucu, istemciye
gerçek RTT'yi ölçmeden bilemez); `loadPercent`, bu düğümün kendi tünel
havuzu doluluk oranından **gerçek zamanlı** hesaplanır, kardeş düğümler için
ise `/internal/v1/status` yoklamasından gelir.

## POST /api/v1/connect

İstek (`ConnectRequestDto` + ek alan):

```json
{
  "serverId": "tr_ist_01",
  "protocol": "WIREGUARD",
  "clientTimestamp": 1735689600000,
  "clientPublicKey": "base64-x25519-pubkey"   // EK, opsiyonel
}
```

`protocol` şunlardan biri olmalı: `WIREGUARD`, `OPENVPN_UDP`, `OPENVPN_TCP`,
`IKEV2` (desteklenmiyor — aşağıya bakın).

**clientPublicKey verilmezse** ("bootstrap modu", mevcut uygulamanın davranışı):
sunucu istemci için de bir WireGuard anahtar çifti üretir ve özel anahtarı
yanıtta döner (yalnızca bu bir seferlik). **clientPublicKey verilirse** (bir
istemci güncellemesi bunu eklerse): özel anahtar hiçbir zaman cihazdan çıkmaz
— tercih edilen, daha gizlilik-dostu yoldur.

Yanıt (`ConnectResponseDto` + ek alanlar), WireGuard başarılı olduğunda:

```json
{
  "success": true,
  "sessionId": "sess_tr_ist_01_3f9a...",
  "status": "ESTABLISHED",
  "virtualIp": "10.66.0.2",
  "serverId": "tr_ist_01",
  "assignedPort": 51820,
  "handshakeDurationMs": 42,
  "message": "WireGuard tüneli sunucu tarafında kuruldu.",

  "protocol": "WIREGUARD",
  "virtualIpv6": "fd00:66::a42:2 (yalnızca çift yığın düğümlerde)",
  "serverPublicKey": "base64-server-pubkey",
  "clientPrivateKey": "base64-client-privkey (yalnızca bootstrap modunda)",
  "endpoint": "vpn.example.com:51820",
  "allowedIps": "0.0.0.0/0, ::/0",   // v4-only düğümde yalnızca "0.0.0.0/0"
  "dns": ["10.66.0.1"],
  "mtu": 1420,
  "persistentKeepaliveSeconds": 25
}
```

OpenVPN (`OPENVPN_UDP`/`OPENVPN_TCP`) için `status` her zaman `"PROVISIONED"`
olur (istemci henüz bağlanmadı, yalnızca kimlik bilgisi hazırlandı) ve yanıt
ek olarak `ovpnProfile` içerir: tamamen kendi kendine yeten, gömülü
sertifikalı bir `.ovpn` profili metni.

`IKEV2` için de yanıt `"PROVISIONED"` olur ve ek alanlarla birlikte gelir:

```json
{
  "ikev2ServerId": "vpn.example.com",
  "ikev2Username": "sess_tr_ist_01_3f9a...",
  "ikev2Password": "tek kullanımlık EAP parolası",
  "ikev2CaCertPem": "-----BEGIN CERTIFICATE-----\n..."
}
```

Bunlar, işletim sisteminin yerleşik **IKEv2/IPsec MSCHAPv2** profiline
girilecek değerlerdir; `ikev2ServerId`, sunucu sertifikasının doğrulanacağı
kimliktir. Kullanıcı adı oturum kimliğiyle aynıdır ve o oturuma özel
strongSwan bağlantısı **yalnızca bu kimliği** kabul eder — başka bir
oturumun parolası bu bağlantıda çalışmaz. IKEv2 isteğe bağlıdır
(`install.sh --with-ikev2`); kapalıysa yanıt `503 UNAVAILABLE` olur.

HTTP durum kodları: `200` başarı, `400` geçersiz istek (bilinmeyen
`serverId`/protokol/anahtar), `429` hız sınırı, `503` kapasite dolu ya da
ilgili tünel arka ucu bu düğümde etkin değil, `500` beklenmeyen hata,
`502` bir kardeş düğüme iletilirken o düğüme ulaşılamadı.

> **Bilinen istemci davranışı notu**: mevcut `VpnRepository.kt`, 2xx dışı
> her yanıtı veya istisnayı **yerel bir simülasyona** düşürür ve
> `ConnectResponseDto.success` alanını hiç okumaz. Yani bugünkü uygulama,
> gerçek bir hata (ör. IKEv2 desteklenmiyor) ile karşılaştığında sessizce
> sahte bir bağlantı gösterecektir. Bu, istemci tarafında (bu görevin
> kapsamı dışında) düzeltilmesi gereken bir sınırlamadır; sunucu tarafı
> kasıtlı olarak dürüst durum kodları/`success` değerleri döner.

## POST /api/v1/disconnect

```json
{ "sessionId": "sess_...", "reason": "USER_REQUEST" }
```

Her zaman `{"success": true, "message": "..."}` döner — bilinmeyen/süresi
dolmuş bir `sessionId` de "zaten sonlanmış" olarak başarılı sayılır
(idempotent).

## GET /api/v1/telemetry?sessionId=...

```json
{
  "serverId": "tr_ist_01",
  "status": "CONNECTED",
  "virtualIp": "10.66.0.2",
  "downloadSpeedMbps": 42.7,
  "uploadSpeedMbps": 11.3,
  "totalDownloadedBytes": 5242880,
  "totalUploadedBytes": 1048576,
  "sessionDurationSeconds": 125,
  "serverHealth": "OPTIMAL",
  "trafficSamples": [12.1, 15.4, ...]
}
```

Hız değerleri, art arda iki `wg show dump` / OpenVPN `status 3` okuması
arasındaki **gerçek** bayt sayacı farkından hesaplanır — simüle edilmez.
`serverHealth`: `OPTIMAL` (aktif el sıkışma), `DEGRADED` (WireGuard peer'ı
3 dakikadan uzun süredir el sıkışmadı), `CONNECTING` (OpenVPN kimliği
verildi ama istemci henüz bağlanmadı). Bilinmeyen `sessionId` → `404`.

### `totalDownloadedBytes` / `totalUploadedBytes` aslında ARTIŞTIR

Adlarına rağmen bu iki alan, **bir önceki yoklamadan bu yana** taşınan
bayt sayısıdır; kümülatif sayaç değildir. Adlar istemciye aittir ve
istemci toplamı kendisi tutar:

```kotlin
// viewmodel/VpnViewModel.kt, startLiveServerTelemetry
totalDownloadedBytes = state.totalDownloadedBytes + telemetry.totalDownloadedBytes
```

Uygulama saniyede bir yokladığı için kümülatif sayaç göndermek, kullanıcının
gördüğü toplamı karesel büyütür: 100 MB'da duran bir oturum bir dakika
sonra ~3 GB, bir saat sonra ~180 GB görünür.

Bağlantıdan sonraki **ilk** yoklama örnekleme temelini kurar ve tasarım
gereği 0 döner (istemcinin ekleyeceği bir geçmiş yoktur). Tünel yeniden
başlarsa arka uç sayacı sıfırlanır; bu durumda o yoklama negatife düşmek
ya da uint64 taşması yaşamak yerine 0 döner ve bir sonraki yoklamada
normale döner.

### İstemcinin katı olduğu noktalar

Uygulama Moshi'yi `KotlinJsonAdapterFactory` ile kuruyor
(`data/VpnRepository.kt`). Uygulamanın gerçek DTO'larıyla ve gerçek
Moshi 1.15.2 ile ölçüldü:

| Durum | Sonuç |
|---|---|
| Bilinmeyen fazladan alan | **Yok sayılır** — yanıta alan eklemek güvenli |
| `categoryNames: null` ya da alan hiç yok | `JsonDataException` — **tüm** `/servers` yanıtı çöker |
| `trafficSamples: null` | `JsonDataException` — telemetri yanıtı çöker |
| Varsayılanı olmayan bir alan eksik (ör. `virtualIp`) | `JsonDataException` |
| `message: null` | Sorun yok (alan nullable) |
| `/settings` yanıtında boolean olmayan bir değer | `JsonDataException` (`Map<String, Boolean>`) |

Bunlar sessiz arızalardır: `VpnRepository` istisnayı yutar ve sunucu
listesinde **sabit kodlanmış demo listesine** (`defaultServers`), telemetride
ise simüle edilmiş değerlere düşer. Kullanıcı gerçek sunucuya bağlı
olduğunu sanır. Bu yüzden `categoryNames` ve `trafficSamples` sunucu
tarafında asla `null` olmayacak şekilde normalize edilir.

### Zaman aşımı bütçesi: 3 saniye

İstemcinin OkHttp bağlantı/okuma/yazma zaman aşımı **3 saniye**
(`data/VpnRepository.kt`). `/connect` bu süreyi aşarsa uygulama istisnayı
yakalar ve **simüle edilmiş** bir bağlantı üretir: kullanıcıya "bağlandı"
gösterilir ama ortada tünel yoktur. Bu yüzden `/connect` yolundaki her iş
bu bütçeye göre tasarlanmıştır (bkz. `docs/ARCHITECTURE.md`, sertifika
algoritması seçimi).

## POST /api/v1/settings

```json
{ "killSwitch": true, "threatProtection": true, "splitTunneling": false, "autoConnect": false }
```

**Önemli yorum notu**: bu DTO'da bir oturum/kullanıcı kimliği yok. Bu nedenle
ayarlar, oturum başına değil, **bu düğümün genel varsayılan politikası**
olarak uygulanır:

- `threatProtection` → unbound'un reklam/izleyici engelleme listesi anında
  açılır/kapanır (`unbound-control reload`).
- `splitTunneling` → **sonraki** WireGuard `connect` çağrılarının
  `allowedIps` alanını etkiler (`0.0.0.0/0, ::/0` yerine yalnızca tünel
  alt ağı).
- `killSwitch` — temelde istemci tarafı bir kavramdır (cihazın VPN
  dışı trafiği engellemesi); sunucu bunu saklar/yanıtlar ama zorlayamaz.
- `autoConnect` — tamamen istemci tarafı; sunucu yalnızca saklar/yanıtlar.

Yanıt, gönderilen değerleri `Map<String, Boolean>` olarak yansıtır.

## GET /internal/v1/status (yalnızca filo içi)

Android uygulaması tarafından **hiç** çağrılmaz. `X-Internal-Fleet-Secret`
başlığıyla korunur (config.json'daki `fleet.sharedSecret`); kardeş düğümlerin
sağlık/yük yoklaması için vardır. Sır yapılandırılmamışsa `503` döner.
