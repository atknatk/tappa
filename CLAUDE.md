# Tappa — Çalışma Kuralları

> **Tappa** — "Punchless time & attendance." NFC tabanlı, cihazsız mesai takip SaaS'ı.
> NFC etiketi çalışanın cebinde değil **duvarda**: her lokasyon girişine pasif bir
> plaket (NTAG 424 DNA) monte edilir. Çalışan kendi telefonunu değdirir → tarayıcı
> açılır (uygulama kurulumu YOK) → kalıcı oturumdan tanınır → tek butonla check-in/out.
>
> Ürün/pazar/marka detayı: **[docs/handoff.md](docs/handoff.md)** — kararların gerekçesi orada.
> Bu dosya *nasıl çalışacağımızı* anlatır, *ne inşa ettiğimizi* değil.

---

## 1. Stack — sabit, tartışmaya kapalı

| Katman | Seçim | Gerekçe |
|---|---|---|
| Dil | **Go 1.26** | tek binary, statik deploy |
| Router | **chi** (stdlib `net/http` üstüne) | framework kilidi yok, `http.Handler` uyumlu |
| DB sürücü | **pgx/v5** (pool) | Postgres'e en yakın, `database/sql` katmanı yok |
| Sorgu | **sqlc** — SQL yazılır, Go üretilir | ORM YOK; RLS ve atomik `ctr` update'i birebir kontrol |
| Migration | **goose** (`db/migrations/*.sql`) | düz SQL, geri alınabilir |
| HTML | **templ** (tip güvenli) + **HTMX** | tek binary, SPA yok |
| CSS | **Tailwind standalone CLI** | **Node YOK** — `.tools/tailwindcss` ikilisi |
| Deploy | tek VPS (AB bölgesi) + managed Postgres | mikroservis YOK |

**Node/npm/node_modules bu repoda ASLA yer almaz.** Bir çözüm Node gerektiriyorsa, o
çözüm yanlış çözümdür — alternatif ara veya sor.

Yeni bağımlılık eklemeden önce sor. `stdlib > küçük kütüphane > framework`.

## 2. Komutlar

```bash
make up            # postgres ayağa kalkar
make migrate seed  # şema + KF/KM demo verisi
make dev           # gen + css + çalıştır → localhost:8080
make gen           # templ + sqlc üretimi   ← .templ veya .sql değişince ZORUNLU
make test          # go test -race ./...
make check         # fmt + lint + test (CI'nin çalıştırdığı)
make audit         # govulncheck + kırmızı çizgi taraması
```

`.templ` veya `db/queries/*.sql` düzenledikten sonra `make gen` çalıştırmadan
"derlemiyor" deme. Üretilen dosyalar (`*_templ.go`, `internal/store/*.go`)
**elle düzenlenmez**, ama commit **edilir**.

## 3. Dizin haritası — nereye ne yazılır

```
cmd/tappa/            main; wiring ve graceful shutdown. İş mantığı YOK.
internal/config/      env okuma + doğrulama. Eksik zorunlu env = başlangıçta panic.
internal/httpx/       router, middleware (request id, tenant, auth, real-ip, rate limit)
internal/handler/     HTTP handler + DTO. Kural: parse → domain çağır → render.
                      Handler içinde iş kuralı veya SQL YOK.
internal/handler/operator/  platform operatörü yüzeyi (/operator, ayrı host); müşteri
                      paneli bu paketi import etmez. DB'si db.OperatorDB (ADR 0020/0021).
internal/policy/      ⭐ policy motoru. Guardrail (kapatılamaz) + baseline +
                      tenant politikaları; effect döndürür, kayıt yazmaz.
                      Saf fonksiyon. Detay: docs/plan/m3-policy-motoru.md
internal/domain/tap/  ⭐ tap karar motoru. §5 buranın spec'i. Saf fonksiyon,
                      DB/HTTP bilmez → tablo bazlı testlerle kanıtlanır.
internal/domain/tenant/  tenant/lokasyon/departman/çalışan iş kuralları; dış API'nin yazma işleri
                      (`PutVenueTx`, `staffapi.go`) ve punch akışının okuma ucu (`PunchFeed`, M11 API-7)
internal/sun/         NTAG 424 DNA SDM doğrulama: AES-CMAC + ctr. Kriptografi
                      SADECE burada. Skill: `tappa-sun`
internal/session/     oturum token üretimi/doğrulama (hash saklanır, token değil)
internal/operatorauth/ platform operatörü kimliği (ADR 0020): parola, TOTP, oturum,
                      çerez, limiter. Tenant kapsamsız; DB'ye yalnız op_* ile gider.
internal/store/       sqlc ÜRETİMİ — elle dokunma
internal/geo/         haversine, GPS yarıçap kontrolü
internal/brand/       tenant markasının çekirdeği (ADR 0023/0024): accent WCAG kapısı
                      (ParseAccent, Check, Suggest), tema gövdesi, logo kapısı (Normalize:
                      PNG/JPEG, decode'dan önce sınır, yeniden kodlama; bir decode semaforu
                      taşır). DB, log, sır yok.
internal/netx/        adres aralığı aritmetiği (bir liste yer kanıtı olamayacak
                      kadar geniş mi). Saf: `net/netip` + `math/big`, başka hiçbir
                      şey — bu yüzden hem tenant hem tap ondan çağırabilir.
internal/qrcode/      QR kodlayıcı (ISO/IEC 18004, byte modu, sürüm 1-40), yalnız stdlib — operatör kayıt QR'ı (ADR 0020 §3).
internal/apikey/      dış API anahtarı (M11 API-3): üretim, biçim kontrolü, HMAC; çözümleyicinin
                      taşıdığı olguyu (iptal, API erişimi) değerlendirir; owner paneli eylemleri.
                      Anahtar her yazdırma yolunda yer tutucu basar; log yok (ADR 0028 §3).
                      Idempotency çalıştırıcısı da buradadır (`Writes`: tek işlem — talep → iş →
                      sonuç; ADR 0028 §8).
internal/handler/api/ /v1 yüzeyi (M11 API-4), ayrı host `TAPPA_API_HOST` — isteğe bağlı, yoksa
                      `/v1` her host'ta 404: iki yönlü host kapısı, kimlik zinciri, JSON hata,
                      64 KiB sınırı, askı kontrol noktası, openapi.yaml. Müşteri paneli import
                      etmez, o da paneli import etmez (ADR 0028 §13). Uçların işi panelin domain'inden
                      geçer (ör. `PUT /v1/locations` → `tenant.Venues`); API kanıt (IP/GPS) yazmaz.
internal/invite/      davet basımı ve gönderimi; K-17 davet kuyruğu işçisi (M11 API-6 — `invite_requests`,
                      kira 60 sn, kod **gönderim anında** basılır, kuyrukta kod yok; `webhook.Worker`'ın işi).
                      K-22 kapısını, kesiciyi ve tavanları basımdan **önce** okur; istek en çok 72 sa bekler.
internal/webhook/     K-19'un süreç içi "işi olan tenant'lar" kümesi ve işçi iskeleti (M11 API-4)
                      — tenant'lar arası tarama YOK; uç nokta ve mühürlü imza anahtarı API-9'da,
                      teslim (kira, geri çekilme) API-10'da (ADR 0028 §6). İmza (`X-Taptime-Signature:
                      sha256=…`) ve `tt_whsec_` anahtarı burada (`signing.go`).
internal/webhook/endpoints/  sahibin tek uç noktası (M11 API-9): adres kuralı, `TAPPA_WEBHOOK_KEK` ile
                      mühürlü anahtar (bir kez gösterilir), ping, ping + "yeniden gönder" kovası
                      (`audit_log`'dan, saatte 10). Ayrı paket: `internal/webhook` DB okumaz.
internal/egress/      SSRF-güvenli giden HTTP istemcisi (M11 API-9): dial anında izin listesi (IANA
                      kaydı birebir + düğüm/ingress listesi), Proxy nil, yönlendirme yok, 10 sn, gövde
                      okunmaz; sonuç yalnız kapalı sınıf + durum. Egress açıkken tek savunma (ADR 0028 §7).
  — aşağıdakiler HENÜZ YOK, M11'de doğacak (ADR 0027/0028) —
internal/sandbox/     sandbox simülasyonu; YALNIZ `sandbox` build tag'li cmd dosyası import
                      eder — prod derlemesinde yoktur (ADR 0028 §11).
internal/db/holding.go  kapalı listedeki holding_* çağrıları, elle ve tipli (resolve.go
                      emsali); ayrı bir holding paketi yok (ADR 0027 §2).
internal/domain/ledger/site.go  işverenin okuyucularında eş mekân satırını adlandırır ve
                      fişte/kuyrukta K-39'u uygular (ADR 0027 §9; kapsam aşağıda §9).
internal/db/feedtimeouts.go  olay yazan tetikleyicilerin (punch akışı) takılı yazıcı zaman
                      aşımları; açılışta bir kez okunur (ADR 0028 §5).
db/migrations/        goose SQL. Uygulanmış migration DEĞİŞTİRİLMEZ, yenisi yazılır.
db/queries/           sqlc kaynak SQL
web/templates/        .templ dosyaları
web/static/           css/img/js — Go binary'sine embed edilir
docs/adr/             mimari kararlar (NNNN-baslik.md)
test/fixtures/        seed.sql + test vektörleri
```

## 4. Kırmızı çizgiler — ihlal edilemez

Bunlar tercih değil, ürünün varlık sebebi. Bir görev bunlardan birini ihlal
ediyorsa **dur ve sor**, sessizce uygulama.

1. **Biyometrik veri asla toplanmaz/saklanmaz.** Parmak izi, yüz, ses — hiçbiri.
   Telefonun kendi ekran kilidi bizim katmanımız değildir; ondan veri almayız.
2. **GPS yalnızca tap anında okunur.** Arka plan konum, sürekli takip, geofence
   izleme YOK. Bu hem GDPR gereği hem satış argümanı.
3. **`transactions` immutable.** UPDATE/DELETE yok. Düzeltme = yeni kayıt +
   `audit_log`. Mesai kaydı hukuki delil olabilir. Dışa bakan izdüşümleri de append-only'dir:
   `punch_events` (ADR 0028 §4) ve `site_punches` (ADR 0027 §9). `site_punches`'ı yalnız `holding_feed`
   yazar; `punch_events`'i iki tetikleyici (`holding_feed`, `punch_feed`) ve `00050` geri doldurması
   yazar — üçü dışında yazan yoktur. `00050` yeniden koşulabilir (yalnız olayı olmayan punch'ı
   yazar); planı istatistiğe değil sorgunun metnine bağlıdır (`TestFeedBackfill_ThePlanIsTheTexts`). `punch_events.direction` (`in`/`out`) işverenin
   `transactions.type`'ıdır; onay olayı ana kaydın yönünü taşır (K-40, kullanıcı kararı 2026-10-14).
4. **`ctr` kontrolü atomik.** Oku-sonra-yaz replay açığıdır. Tek ifadede:
   `UPDATE tags SET last_ctr=$2 WHERE uid=$1 AND last_ctr < $2 RETURNING …`
   Etkilenen satır 0 ise → REJECT. Bunu asla transaction dışı iki adıma bölme.
   Holding'deki ikinci kopya `holding_advance_tag_counter` (ADR 0027 §2) aynı tek ifade
   kalıbıdır (`FOR UPDATE` + katı `<`); SQL düzeyindeki yarış testi ikisini birlikte koşar.
   `make audit`'in R4'ü sayaç yazmasını ifade olarak okur (çok satırlı, takma adlı; `_test.go`
   hariç), ama kanıt değildir — garanti katı yüklem, `tags_counter_monotonic` tetikleyicisi ve
   yarış testleridir.
5. **Tenant izolasyonu her katmanda.** Her tabloda `tenant_id`, her tabloda RLS
   politikası, uygulama `tappa_app` rolüyle bağlanır (NOBYPASSRLS, tablo sahibi
   değil). Sorgularda ayrıca açık `tenant_id` filtresi — kuşak+kemer.
   **Bilinçli istisnalar iki tanedir** — tenant sınırını aşmanın yalnız bu iki yolu var:
   **(1) `op_*`** (ADR 0021) — platform operatörünün SECURITY DEFINER fonksiyonları;
   `tappa_operator` yalnız onları çağırır, tenant verisine her erişim
   `operator_audit_log`'da iz bırakır; operatör kimliği ayrıdır ve tenant kapsamsızdır
   (ADR 0020). **(2) Aynı holding'deki tenant'lar arası dokunuş ve görünürlük** (ADR 0027)
   — yalnız ADR 0027 §2'deki **kapalı** `holding_*` fonksiyon listesi ve holding
   tetikleyicileri (`tappa_holdingdefiner`); holding üyeliğini yalnız `op_*` yazar; holding
   dışı bir tenant bu yüzeye hiçbir yoldan erişemez (tek, sınırlı istisna: bir üyelik bittikten
   sonraki 60 sn — K-29'un çıkış penceresi — içinde toleranslı kollardan yazılan satırlar **iki
   yönde de** karşı tarafın izdüşümüne ve akışına düşer: çıkandan kalana, kalandan çıkana), eş bir
   tenant holding dışı bir tenant'ın anahtarını (plaket uid'i, lokasyon id'si) veremez. Saha
   görünürlüğü `site_punches` izdüşümüdür (append-only); onu yalnız `holding_feed` tetikleyicisi
   yazar, sahanın okumaları sıradan RLS'li sorgulardır. Bu iki istisnanın dışında kalan dış akış
   yazıcısı `tappa_feedwriter`'dır (NOLOGIN, **NOBYPASSRLS**, üyesiz; ADR 0028 §4): kendi-tenant
   olay tetikleyicisinin sahibidir, tenant sınırını aşmaz; holding punch'ının iki olayını holding
   tetikleyicisi yazar (K-31). Bir işlem tek bir sayaç kümesine olay yazar — sayaç kilit sırası
   buna dayanır ve kural mekaniktir: kümenin dışında olay yazan ikinci satır kilitten önce
   `SQLSTATE TF001` ile reddedilir (ADR 0028, API-2 uygulama notu). İşareti
   (`tappa.feed_counters`) önceden yazan bir işlem kontrolü atlatır ve başka tenant çiftlerinin
   holding dokunuşlarını 40P01'e sürükler — o kayıt yazılmaz; ürün kodunda bu ayarı yazan yol
   yoktur ve redline R5c adı `db/migrations/` dışında reddeder. **Listeye
   fonksiyon eklemek, bir fonksiyonun dönüş sütunlarını ya da definer'ın sütun matrisini
   genişletmek, `holding_peer`'e kip eklemek ya da bir kipin süzgecini veya zaman kuralını
   gevşetmek kullanıcı onayı + ADR 0027 değişikliği ister** (ADR 0027 §2; testler: ADR 0027
   §2'deki imza katalog testi ve sütun matrisi testi).
6. **Kayıt asla kaybolmaz.** Kanıt yetersizse REJECT edip atma: kaydı yaz,
   `verdict='flag'` ver, müdür onay kuyruğuna düşür. Sessiz onay da yok.
   Holding dokunuşunun site satırı ve olay yazan her satır checkin'de savepoint içinde yazılır ve
   **yalnız tap yolunda**, yalnız kapının (`holding_site_gate`), izdüşümün (`holding_feed`) ya da
   akışın (`punch_feed`) 55P03'ünde **bir kez** yeniden denenir; iki beklemeden (> ~4 sn) uzun bir
   kilit adlandırılmış bedeldir (ADR 0027 §13 madde 16, 18, 23). Sahanın `tags`/`locations` satırlarını `transactions`'ın kendi anahtarları
   bekler — orada süre sınırı yoktur, sınır isteğin zaman aşımıdır. Bu tablolara dokunan migration
   ve el işi kendi `lock_timeout`'unu koyar (`deploy/README.md`).
7. **NTAG AES anahtarları repoda yer almaz.** DB'de KEK ile sarmalanmış saklanır
   (`tags.aes_key_ref`), log'a asla yazılmaz. Test vektörleri ayrı ve sahte.

`make audit` bu maddeleri mekanik olarak tarar; geçmesi ihlal olmadığını kanıtlamaz.

## 5. Tap karar motoru — spec

Bu bölüm `internal/domain/tap` **ve** `internal/policy` baseline'ı için normatif
metindir. Kod bundan sapıyorsa kod yanlıştır.

Kararlar kod içi `if` zinciri değil, **policy motorundan** gelir:
**satır 1–5 guardrail** (`sys:*`, kodda gömülü, hiçbir müşteri kapatamaz) ·
**satır 6–7 baseline** (`base:*`, tenant değiştirebilir). Hiçbir politika
eşleşmezse sonuç `flag`'dir — sessiz onay imkânsız (§4.6). `tap.Decide` bağlamı
kurar ve dönen effect'i uygular; yön, trust ve geç kalma **hesaptır**, politika
değildir.

**Dört kanıt:** SUN = *şu an fiziksel dokunuş* · oturum çerezi = *kim* ·
statik IP = *nerede* · GPS = *yedek nerede*.

Kanıt **anlamıyla** tanımlıdır, biçimiyle değil: bir lokasyonun kayıtlı adres
aralıkları **bir mekânın ağının olabileceğinden geniş** ise o liste *"nerede"*
hakkında hiçbir şey söylemez, bu yüzden **IP eşleşmesi sayılmaz**. Ama satır 6
bir **AYRIK**tır (*"IP eşleşti **veya** GPS < 150 m"*), dolayısıyla düşen yalnız
**IP yarısıdır** ve sonucu kanal ile GPS belirler — dört şeklin dördü de ölçüldü
(2026-08-20; liste `{0.0.0.0/1, 128.0.0.0/1}`, kaynak `203.0.113.7`):
**NFC + GPS eşleşmesi** satır 6'yı GPS yarısıyla **sağlar** → `ok`, `trust=50`,
not *"verified via GPS only"* (`base:gps-only-allow`) · **NFC + GPS yok** →
satır 7 → `flag`, `trust=20` (`base:no-evidence-review`) · **QR + GPS eşleşmesi**
→ GPS tek başına yetmez (§5'in kendi QR cümlesi) → `flag`
(`base:qr-requires-ip`) · **QR + GPS yok** → `flag` (`base:no-evidence-review`).
**Dördünde de kayıt yazılır** (§4.6). Ölçü **büyüklüktür**,
kapsama değil: aile başına en çok **bir ISP tahsisinin iki katı** — IPv4'te bir `/7`
(2^25 adres), IPv6'da bir `/31` (2^97). *"Dışarıda bir şey bırakıyor mu"* diye sormak
üç turda üç kez aşıldı, çünkü bir uzayı **isim sayarak** tanımlıyordu: `10.0.0.0/8`
tümleyeni kapatıldıktan sonra `25.0.0.0/8` tümleyeni (tek hane farkla) ve
`192.0.2.0/24` tümleyeni (24 satır, dışarıda kalan blok hiç yönlendirilmez) hâlâ
kabul ediliyordu. Kural **yazma** tarafında (mekân kaydedilemez) ve **okuma**
tarafında (böyle bir liste kanıt sayılmaz) aynı fonksiyonla uygulanır; okuma tarafı
gerekli çünkü `transactions` **değişmezdir** (§4.3) — yazma kapısı geçmişte
kaydedilmiş bir aralığı düzeltemez. (`internal/netx`, backlog T40.)

**Karar sırası — ilk eşleşen kazanır:**

| # | Koşul | Sonuç |
|---|---|---|
| 1 | Etiket `lost` veya `retired` | `reject` |
| 2 | SUN geçersiz (CMAC uyuşmuyor **veya** `ctr` artmadı) | `reject` |
| 3 | Oturum yok / geçersiz | aktivasyon sayfası (kayıt yok); onaylı aktivasyon bekleyen tarayıcıda **aktivasyon dokunuşu** (kayıt yok, ADR 0026) |
| 4 | Çalışan `deactivated` | `reject` + denemeyi logla + güvenlik uyarısı |
| 5 | Aynı **kişi** 60 sn içinde tekrar | `ignored` |
| 6 | IP eşleşti **veya** GPS < 150 m | `ok` (IP yoksa not: `verified via GPS`) |
| 7 | ikisi de yok | `flag` — kayıt alınır, onay kuyruğuna düşer |

**Holding (ADR 0027) — tablonun numaraları değişmez, iki ek kural.** Tablonun önünde §4.5'in
uygulaması olan `sys:tenant-mismatch` durur (guardrail listesinde #1): plaketin tenant'ı oturumunkinden
farklıysa `redirect`, **hiçbir** tenant'a kayıt yok — **ama yalnız iki tenant aynı holding'de eş
değilse** (`TagTenantID ≠ SessionTenantID ∧ ¬SameHolding`). Eşse dokunuş bir **holding
dokunuşudur**: kayıt **işverende** (oturumun tenant'ı) yazılır, `site_*` sütunları plaketin
tenant'ını adlandırır; karar **işverenin** politikalarıyla verilir (sahanın lokasyon istisnaları
uygulanmaz); debounce (satır 5) ve yön zinciri işverendedir ve iki tenant'ın plaketlerini birlikte
kapsar. `SameHolding` bir sunucu olgusudur (sayaç ilerletmesinin sonucu ya da katı
`holding_tap_site`), motor onu hesaplamaz. **Yeni guardrail `sys:cross-tenant-qr-site-ip` (K-1g):**
kanal `qr` ∧ holding dokunuşu ∧ **saha IP'si eşleşmedi** → `flag`; yeri `sys:person-debounce`'tan
(satır 5) **hemen sonra**, baseline'dan (satır 6–7) önce; işveren onu ne gevşetebilir ne `deny`'e
sıkılaştırabilir. **Aktivasyon** (satır 3) yalnız davetin **kendi** tenant'ının plaketinde
tamamlanır — ADR 0026 değişmez; canlı oturumlu ve aktivasyonu bekleyen bir tarayıcıda eş tenant'ın
plaketi aktivasyonu değil holding dokunuşunu üretir (K-35). **Sayaç atlaması** (`ctr_gap`)
holding NFC dokunuşunda ilerletme ifadesinin içinde hesaplanır ve işverenin `base:ctr-gap-review`'ı
onu tek-tenant'taki anlamıyla uygular (kullanıcı kararı K-1i, 2026-10-14).

- **Debounce KİŞİ bazlıdır, etiket bazlı değil.** Sıra halinde farklı kişiler
  aynı plakete art arda dokunabilmeli.
- **Debounce'un "60 sn"si SUNUCU zamanıdır** (ADR 0006, 2026-08-01). Mesafe iki
  ölçünün **küçüğüdür**: istemcinin beyan ettiği `occurred_at` farkı **ve**
  veritabanının hesapladığı kayıt yaşı (`clock_timestamp() − created_at`, yalnız
  tap kanalları; `manual` öncül muaf — müdürün yazdığı satır dokunuş değildir).
  Yalnız beyana bakmak dört ayrı şekilde aşılabiliyordu (ölçüldü): geriye tarihleme ·
  öncülü seçme (`ORDER BY occurred_at`) · ileri tarihleme (negatif mesafe guardrail'i
  kapatıyordu) · eşzamanlılık. Sonuncusu için karar + kayıt **kişi başına advisory
  kilit** altında tek transaction'da koşar. **QR'da bu tek frendir** — sayaç yok.
- **Güven puanı:** `20 (taban) + 50 (IP eşleşti) + 30 (GPS eşleşti)`.
- **Yön (in/out):** kişinin **son açık girişine** göre toggle — takvim gününe göre
  DEĞİL. Rusty Bar 18:00–02:00 vardiyasında 02:00 çıkışı doğru eşleşmeli.
- **Geç kalma** çalışanın KENDİ vardiyasına göre: departman vardiyası varsa o,
  yoksa **tap edilen** lokasyonun vardiyası (profildeki lokasyon değil — zincirde
  şube değişimi normaldir). Farklı şubede tap nota işlenir, raporda ayrı görünür.
- **Unutulan çıkış:** sistem çıkış kaydı **üretmez**. Açık kayıt anomali olarak
  listelenir, müdür manuel girer. Açık kayıtlar saat toplamına **girmez** ve
  raporda toplamın eksik olduğu açıkça söylenir.
- **QR kanalı** (`channel='qr'`): SUN yok → **IP zorunlu**, GPS tek başına yetmez
  → `flag`. QR fotoğraflanır ve süresiz geçerlidir; fiziksel dokunuş kanıtı yok.
  Baseline politikası `base:qr-requires-ip`, tenant değiştirebilir.
- **Aktivasyon dokunuşu** (practice tap'in yerini aldı — ADR 0026): davet linki bir
  sihirbaz açar; onay POST'u yalnız onayı kaydeder ve bu tarayıcıya bağlar (daveti
  tüketmez, oturum vermez). Aktivasyon, o tarayıcıdan gelen ilk **gerçek NFC**
  dokunuşunda tamamlanır: `sun.Verify` (atomik `ctr`) → işverenin **aktif** bir
  plaketi (aynı tenant) → tek ifadeli tüketim → oturum. Bu dokunuş **`transactions`
  satırı yazmaz**; sonraki dokunuş sıradan check-in'dir. Yeni kayıt `practice=true`
  olmaz; tarihsel practice satırları saate sayılmaz ve yön zincirine girmez (ADR 0008).
- **Manuel kayıt:** `channel='manual'` + `entered_by` dolu; raporlarda ayrı görünür.
- **API'nin açtığı lokasyon** (`PUT /v1/locations`, M11) kanıtsız doğar: panelden IP/GPS tanımlanana
  kadar dokunuşları satır 7'ye (`flag`) düşer, kayıt yine yazılır. Lokasyonun `active` etiketi karar
  motoruna girmez (K-20).

Bu tablodaki her satırın — ve yukarıdaki iki holding kuralının — adı `TestDecide_...` ile
başlayan bir test durumu olmalı.

## 6. Veritabanı kuralları

- Her yeni tablo şunlarla doğar: `tenant_id uuid NOT NULL`, `ENABLE ROW LEVEL SECURITY`,
  `FORCE ROW LEVEL SECURITY`, `tenant_id` üzerinde politika, `tenant_id` üzerinde indeks.
  Beşinden biri eksikse migration eksiktir (GRANT dahil; bkz. docs/plan/m1-veri-katmani.md).
- **Tenant kapsamsız tablo** (yalnız platform operatörü — ADR 0020/0021 — ve holding
  üyeliği: `holdings`, `holding_members`, yazarı yalnız `op_*` — ADR 0027): R5
  muafiyetiyle **görünür** (`scripts/redline-check.sh`), `tappa_app`'tan **açık**
  `REVOKE ALL` (dev/prod varsayılan yetkileri yeni tabloya yetki verir — ölçüldü),
  `ENABLE` **ve** `FORCE ROW LEVEL SECURITY` birlikte (`scripts/pg-restore-verify.sh`
  ikisinin sayısı farklıysa geri yüklemeyi reddeder). Tenant verisi olmayan bir sütunun
  adı `tenant_id` **olmaz** — R5 ve RLS testleri tabloyu o addan tenant tablosu sayar.
- **`arwd` varsayılanına güvenilmez** (M11'de kondu, **kalıcıdır** — her yeni tablo ve
  fonksiyon; ADR 0028 §1). Üretimde
  `pg_default_acl` yeni tabloyu `tappa_app`'e dört fiille (`arwd`) doğurur; eskiden taze CI'da
  `ar` idi ve CI fazla yetkiyi göremiyordu. Bu yüzden her yeni tabloda `REVOKE ALL … FROM tappa_app` + yalnız
  gereken fiil ve **sütunlara** açık `GRANT`; **UPDATE hep sütun listesiyle** (tablo düzeyi
  UPDATE sütun kısıtını ezer). Her yeni fonksiyonda `REVOKE ALL ON FUNCTION … FROM PUBLIC`;
  tetikleyici fonksiyonlarında `EXECUTE` yalnız sahipte. Taze hacimde (dev ve CI)
  `scripts/db-init/03-dev-production-default-acl.sql` üretimin `arwd`'ini kurar; katalog testi
  tablo × fiil × sütunu matrisle karşılaştırır ve dar kurulmuş DB'de CI'da düşer, yerelde atlanır
  (eski hacim → `make db-reset`).
- Tenant bağlamı bağlantı başına `SET LOCAL app.tenant_id` ile verilir; politikalar
  **`NULLIF(current_setting('app.tenant_id', true), '')::uuid`** okur. `SET LOCAL` →
  transaction dışında sızmaz. Havuzdan alınan bağlantıda `SET` (LOCAL'siz) **kullanma**.
  `NULLIF` şart, süs değil: GUC'a bir kez **yazıldıktan** sonra bağlantıda `NULL`'a
  dönmez, `''` kalır (`ROLLBACK`/`RESET`/`DISCARD ALL` üçü de) — çıplak cast o
  bağlantıda `NULL` değil **hata** verir, yani davranış bağlantı geçmişine bağlı olur.
  Ölçüm ve gerekçe: docs/plan/open-questions.md → Q27, ADR 0002.
- **İzolasyon testi ile üretim sorgusu farklı şekiller ister.** Üretim sorguları
  §4.5 gereği açık `tenant_id` filtresi taşımak **zorunda**; RLS izolasyon testi
  ise taşımamalı — filtre varsa 0 satırın sebebi RLS değil `WHERE`'dir ve test RLS
  kapalıyken de yeşil kalır (ölçüldü). İkisi çelişki değil, farklı iş.
- **`audit_log.at` yalnız veritabanının `DEFAULT now()`'ıdır:** `tappa_app`'in INSERT'i sütun listesidir,
  `at` hariç (00053, ADR 0028 API-6). K-25 pasifleştirme bütçesi o sütundan sayılır; `at`'i yazması
  gereken bir test bunu owner bağlantısıyla yapar.
- Zaman: DB'de her şey `timestamptz`, **UTC**. Yerel saate çevirme sadece render
  katmanında. Gece vardiyası bug'larının kaynağı budur.
- Para/saat hesabı `float` ile yapılmaz.
- Migration geri alınabilir olmalı (`-- +goose Down` dolu).
- Yeni sorgu = `db/queries/*.sql` + `make sqlc`. Handler içine ham SQL yazılmaz.
- **Sıcak bir tabloya** (ör. `employees`) indeks, Down yürüyüşüne giren bir migration'a değil **ayrı bir
  migration dosyasına** konur — o Down tabloya ACCESS EXCLUSIVE alır ve paralel testleri kilitler
  (API-8, 00056); kendi `lock_timeout`'unu taşır, büyük tabloda `CONCURRENTLY` (`deploy/README.md`).
- API sorgularında sayfa/aralık **sınır ifadeleri çıplak parametredir**: `coalesce(@x, …)` gibi bir
  ifade genel planda indeks koşulu olmaktan çıkıp süzgece döner (API-8 ölçümü: 0,8 ms → 90 ms).

## 7. Go kod kuralları

- Hata sarmalama: `fmt.Errorf("checkin: %w", err)`. Sessizce yutma yok; `_ = err` yok.
- Handler'lar `context.Context` taşır ve iptal eder. Global state, `init()` sihri yok.
- Bağımlılıklar açıkça enjekte edilir (struct alanı), paket seviyesi singleton yok.
- Arayüz **tüketici** tarafında tanımlanır, üretici tarafında değil.
- Log: `log/slog`, yapılandırılmış. **Asla loglanmaz:** oturum token'ı, CMAC,
  AES anahtarı, davet kodu, e-posta adresi (ADR 0022 §10), tam GPS koordinatı; operatör tarafında okuma bileti,
  TOTP kodu ve sırrı, enrollment token'ı (ADR 0020/0021 — hata mesajı ve audit
  `detail`'i dahil); dış API tarafında API anahtarı ve hash'i, `Authorization` başlığı,
  webhook imza anahtarı (`tt_whsec_` önekli), webhook URL'si (host'u ve yolu dahil) ve
  `*url.Error` metni (Go o metinde URL'yi taşır — ADR 0028 §3, §6; bunlarda da hata mesajı ve
  audit `detail`'i dahil).
- Env anahtarları (`namedKeys()`) **çift çift** birbirinden farklı olmak zorundadır — iki anahtarın
  eşitliği açılışı reddeder (ADR 0028 §3; bütün çiftler, yalnız operatör çiftleri değil).
- Dış girdi handler sınırında doğrulanır; domain katmanı zaten geçerli veri görür.
  İstemcinin taşıdığı zaman damgası ya da sayı (imleç alanları dahil) sorguya gitmeden **anlamlı
  aralığa** sınanır, dışı 400: pgx int64 ↔ `timestamptz` dönüşümünde uçlar sessizce sarar (200 + yanlış
  cevap), Postgres aralığını aşan değer 22008 → 503 olur (API-8, O-1).
- Yorumlar *neden*i anlatır. Kodun ne yaptığını tekrar eden yorum yazma.
- Türkçe karakter kodda yok: identifier, log, hata mesajı, commit → İngilizce.
  Kullanıcıya görünen metinler ve bu repodaki dokümanlar Türkçe/İngilizce olabilir.

## 8. Test kuralları

- `internal/domain/tap` ve `internal/sun` için kapsam hedefi **%90+**. Bunlar ürünün
  doğruluk çekirdeği.
- Tablo bazlı testler (`tests := []struct{name string; …}`), her karar satırı için bir vaka.
- `internal/sun` testleri **bilinen cevap vektörleriyle** çalışır: geçerli imza,
  sayaç tekrarı, sayaç geriye gitmesi, yanlış anahtar, bozuk hex.
- Replay koruması için gerçek eşzamanlılık testi: aynı `(tag, ctr)` ile N goroutine →
  tam olarak **1** başarılı. `-race` ile.
- DB testleri gerçek Postgres'e karşı (testcontainers veya `make up`), sahte değil —
  RLS'i sahte veritabanı test edemez.
- **RLS testi zorunlu:** A tenant'ının bağlantısı B tenant'ının satırını görmemeli.

## 9. Arayüz & marka

Detay ve token'lar: **skill `tappa-brand`** (UI'a dokunmadan önce oku).

Özet: paletin dışına çıkma (`ink #152219`, `porcelain #EDF0EA`, `tappa-green #1F5C41`,
`saffron #D98E2B`, `tomato #BE3D2A`, `paper #FFFDF4`); Space Grotesk (display) +
IBM Plex Mono (veri). İmza motif **mutfak adisyonu (kitchen docket)** — işlem kayıtları
perforeli fiş kartı, APPROVED/FLAGGED/REJECTED hafif eğik kaşe damgası.

Çalışan tap ekranı kutsaldır: **tek ekran, tek buton, sıfır öğrenme**. Onay ekranında
buton YOK — "All done — you can close this page." Bir sonraki işlem zaten yeni fiziksel
dokunuş gerektirir. Bu ekrana özellik eklemek istiyorsan önce sor.

**Tenant markası (M10 white-label — ADR 0023/0024, skill `tappa-brand` → "Tenant slotları").** İşletme sayılı slotları boyar: bir logo (tap ve sonuç ekranında 24 px yuva + altında "taptime · punchless", panelde 32 px; aktivasyon ailesinde — sihirbazın dört adımı, "Activation complete", "already set up" — tap ekranının başlığı, ama **yalnız VIES-doğrulanmış** işletmede) ve tek bir accent. Accent'in ürün yüzeyinde boyadığı CSS slotları iki tanedir: tap düğmesinin zemini (tap ekranı ve aktivasyon sihirbazının 1–3. adımlarının birincil düğmesi) ve panelin 4 px şeridi (editörün renk seçicisi kendi rengini gösterir — ADR 0023 §2); accent WCAG kapısından geçer; accent varken sihirbazın öteki yeşil işaretleri (ilerleme çubuğu, 01/02/03, işaretli onay kutusu) ink olur. Kaşe damgaları, durum→renk eşlemesi, docket, sonuç ekranının renkleri, aktivasyonun problem ve onay ekranları ve bekleme ekranının plaket çizimi değişmez; sonuç ekranında accent yoktur. Bu, "paletin dışına çıkma" ve tap ekranı kuralına kullanıcının §9 kararlarıyla (D-C; K-2a, K-2b; aktivasyon ailesi 2026-10-09 — ADR 0023 WL-13 notu) verilmiş sayılı istisnadır — tap/sonuç/aktivasyon ekranlarındaki başka her değişiklik yine önce sorulur. Marka taklidi kabul edilmiş bir risktir (ADR 0005, risk 9).

**Holding plaketinde marka plaketin tenant'ınındır (M11 — ADR 0027 §10, kullanıcı kararı K-1c).** Bir holding dokunuşunda (KM çalışanı KF plaketinde) tap ekranı **KF'nin** logosunu ve accent'ini, sonuç ekranı **KF'nin** logosunu taşır — oturumun tenant'ınınkini değil; logo yalnız VIES-doğrulanmış saha tenant'ında ve `holding_logo` ile. Slotlar, sayılı istisna ve sonuç ekranında accent olmaması aynen geçerlidir; değişen yalnız markanın **sahibidir**. Aktivasyon ailesi değişmez (aktivasyon yalnız kendi tenant'ının plaketinde — ADR 0026). **Saha ve işveren panelinde holding (API-1c — ADR 0027 §9):** sahanın raporu eş personelin dokunuşlarını **süresiz** listeler (süre işverende). İşverenin panelinde eş mekân satırı (K-39) için gösterilmeyenler: fişte ve onay kuyruğunda güven puanı, IP/GPS işaretleri ve **kanıt anlatan kuralın notu** (sid'e göre kapalı küme: `base:`/`own:` × `ip-or-gps-ok`, `gps-only-allow`, `no-evidence-review`, `qr-requires-ip`, `gps-conflict-review`, ve `sys:cross-tenant-qr-site-ip`) — yerine *"Decided by your rules at a partner venue — evidence is not shown."*; manuel kaydın notu ve kanıt dışı notlar (kayıp plaket, sayaç boşluğu, farklı şube, yön) korunur — kural önceliği üzerinden kalan artık ADR 0027 §13 madde 5 (e). Anomali ekranında eş satırları hiçbir kanıt sayısına girmez; kural dağılımında tek, adsız ve **hep sonda** duran bir kovadır (eş satırları kural sorgusuna girmez). Okuyucuların envanteri `TestEvidenceReaders_EveryOneIsClassified`'tadır; holding'siz işletmenin rapor ve anomali sayfası bayt bayt aynıdır (şablon: `TestHoldinglessPanels_AreByteIdenticalToTheGolden`, handler: `TestHoldinglessViews_AreByteIdenticalToTheGolden`). Verdict kaşesi, `NoteIsTenants` etiketi ve kural önceliği adlandırılmış artıktır (ADR 0027 §13 madde 5).

## 10. Çalışma şekli

- **Ana oturum iş yapmaz, organize eder.** Her görev bir alt ajana (model `opus`)
  yaptırılır; ardından **ayrı** bir üçüncü göz ajanı kabul kriterlerini denetler.
  Bulgu varsa düzelttirilip **yeniden** denetlenir. Onay gelmeden görev `done`
  olmaz ve sonrakine geçilmez. Ayrıntı:
  [docs/plan/README.md → Çalışma modu](docs/plan/README.md).
- **Oturuma [docs/plan/state.md](docs/plan/state.md) ile başla** — sıradaki görev,
  blokeler ve sağlık kontrolü orada. Protokolün tamamı: skill `tappa-next`.
- Görev başlarken ilgili bölümü oku (`docs/handoff.md` ürün, bu dosya kural,
  `docs/plan/m*-*.md` görevin spec'i).
- İş bitince **`docs/plan/state.md`'yi güncelle** (ledger + "ŞU AN" + oturum notu).
  Atlanırsa bir sonraki oturum yanlış yerden başlar.
- Küçük ve odaklı değişiklik; ilgisiz refactor'ü aynı commit'e karıştırma.
- Şema, karar motoru veya güvenlik sınırı değiştiyse **ADR yaz** (`docs/adr/NNNN-*.md`).
- İş bitince `make check` çalıştır. Kırmızıysa iş bitmemiştir.
- **Büyük testler yalnız FAZ SONUNDA** (kullanıcı kuralı, 2026-10-08, 2026-10-09'da
  yinelendi: *"her adımda test yapıp çok zaman kaybetme"*). Tur içinde yalnız değişen
  testler `-run` ile, ve saniyelik kapılar (`gofmt`, build, redline). Tam `-race`,
  `go test ./...`, tüm mutasyon seti ve `make check` bir görevde **bir kez** koşar:
  commit'ten önceki son doğrulamada. Her yapıcı ve denetçi brief'ine bu yazılır.
  CI her push'ta zaten tam koşar.
- Commit: `type(scope): özet` — `feat(tap): reject retired tags`. İngilizce, emir kipi.
- **İş feature dalında yapılır** (kullanıcı kararı, 2026-09-09 — bu, *"doğrudan
  `main`'de çalışılır"* diyen 2026-07-24 kararının **yerini alır**). Gerekçe
  ölçüldü: **`main` üretime deploy eder.** `.github/workflows/deploy.yml`
  `workflow_run … branches: [main]` ile bağlı, yani `main`'e atılan her commit
  CI yeşilse **canlıya çıkar**; oysa `ci.yml` `on: push` ile **her dalda** koşar.
  Dal bu yüzden bedava: tam CI doğrulaması verir, deploy riski vermez.
  Kullanıcı istemedikçe **`main`'e birleştirme** — birleştirme bir deploy
  kararıdır, kod kararı değil. Dal push'u serbest, `main` push'u değil.
  (M0 `m0-bootstrap`'ta yapıldı; M1–M8 doğrudan `main`'de; landing yeniden
  tasarımından itibaren yeniden dalda.)
- Emin değilsen sor — özellikle §4 kırmızı çizgilere değen her şeyde.

## 11. Ajanlar & skill'ler

| Ne zaman | Araç |
|---|---|
| Oturum başı/sonu, "sırada ne var", "kaldığımız yerden devam" | skill `tappa-next` |
| Karar kuralı, tenant'a göre değişen davranış, "bunu müşteri ayarlasın" | `internal/policy` — [docs/plan/m3-policy-motoru.md](docs/plan/m3-policy-motoru.md) |
| Güvenlik/kırmızı çizgi denetimi (diff üstünde) | agent `tappa-security-auditor` |
| Yeni tablo/sorgu: migration + RLS + sqlc + test | agent `tappa-db-migrator` |
| Herhangi bir UI/templ/Tailwind işi | skill `tappa-brand` |
| NTAG 424 DNA / SUN / CMAC / ctr | skill `tappa-sun` |
| Demo veri, fixture, "bir günü simüle et" | skill `tappa-seed` |

## 12. Durum

**Canlı durum tek yerdedir: [docs/plan/state.md](docs/plan/state.md).** Bu dosyaya
durum yazma — çelişirler.

Plan sistemi [docs/plan/](docs/plan/) altında: [roadmap](docs/plan/roadmap.md)
(M0–M9: 83 görev; M10'un görevleri [m10-platform.md](docs/plan/m10-platform.md)'de; sıralama gerekçesi) · `m*-*.md` görev kartları ·
[state.md](docs/plan/state.md) (nerede kaldık) ·
[open-questions.md](docs/plan/open-questions.md) (karara bağlanmamışlar).
Nasıl çalıştığı: [docs/plan/README.md](docs/plan/README.md).

⚠️ [docs/handoff.md](docs/handoff.md) §10 yol haritası **Admin Dashboard'u 1.
sıraya** koyar; bu bilinçli olarak değiştirildi (dashboard M6). Gerekçe:
[roadmap.md](docs/plan/roadmap.md#neden-dashboard-1-değil-6-sırada). Handoff'un
ürün içeriği geçerli, değişen yalnızca sıra.
