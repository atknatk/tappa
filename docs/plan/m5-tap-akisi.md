# M5 — Tap akışı (uçtan uca)

**Amaç.** Çalışanın gördüğü ürünü tamamlamak: davet → aktivasyon → plakete
dokunuş → tek buton → onay. Uygulama kurulumu yok, öğrenme yok.

**Bittiğinde:** Gerçek bir telefonla (veya simüle edilmiş SUN URL'siyle) uçtan
uca check-in/out çalışıyor, kayıtlar veritabanında, kararlar §5'e uygun.

**Ana araçlar:** skill `tappa-brand` (her ekran işinde) · skill `tappa-sun` ·
agent `tappa-security-auditor` (milestone sonunda)

> **Tap ekranı kutsaldır** (CLAUDE.md §9): tek ekran, tek buton, sıfır öğrenme.
> Onay ekranında buton **yok**. Bu ekrana özellik eklemek istiyorsan **önce sor**.

---

## M5-01 — `internal/session`: oturum yaşam döngüsü

- **Bağımlılık:** M1-04 · Q11
- **Kırmızı çizgi:** §4.1 (biyometri yok) · §4.7 (token loglanmaz)
- **Commit:** `feat(session): issue, verify and revoke session tokens`

**Amaç.** *Proof of person*: telefonun sahibini tanıyan kalıcı oturum.

**Tasarım.**
- Token kriptografik rastgele (≥32 byte), **yalnızca çereze** yazılır.
- DB'de `token_hash` saklanır (HMAC-SHA256, `TAPPA_SESSION_HMAC_KEY`).
- Çerez: `httpOnly` + `Secure` + `SameSite=Lax` + uzun `Max-Age` (~1 yıl),
  kullanımda yenilenir (`last_used_at`).
- İptal anlık: `revoked_at` damgası → sonraki istek düşer.
- Cihaz limiti (1–2) opsiyonel, config ile kapalı gelebilir.

**Kabul kriterleri.**
- Ham token DB'de, log'da, hata mesajında **hiç** yok — testle kanıtlanır.
- Doğrulama tek sorgu (`GetEmployeeBySessionHash`) ve sabit zamanlı.
- Deaktive çalışanın oturumu anında geçersiz.
- **Q11 ölçüldü:** gerçek bir iPhone'da NFC → Safari akışıyla çerez ömrü
  doğrulandı; sonuç `open-questions.md`'ye yazıldı.

**Tuzaklar.**
- `SameSite=Strict` NFC → tarayıcı açılışında çerezi göndermeyebilir; `Lax` seç
  ve gerçek cihazda doğrula.
- Telefonun kendi ekran kilidi **bizim katmanımız değil** — ondan veri alma,
  WebAuthn/attestation ekleme (§4.1).

---

## M5-02 — Davet ve aktivasyon akışı

- **Bağımlılık:** M5-01 · Q02
- **Kırmızı çizgi:** §4.7 (davet kodu loglanmaz)
- **Commit:** `feat(activation): add invite and one-time activation flow`

**Amaç.** Çalışanın ilk kez tanınması: davet linki → tek kullanımlık kod →
oturum çerezi.

**Kabul kriterleri.**
- Davet kodu tek kullanımlık, süreli, kullanıldıktan sonra ölü.
- Aktivasyon `POST /api/activate` → `Set-Cookie` (httpOnly) → çalışan `active`.
- Kod tahmin edilemez ve **loglanmaz**.
- Zaten aktif çalışanın ikinci aktivasyonu: yeni cihaz mı, saldırı mı — davranış
  belirlenmiş ve testli.
- §5 satır 3 buraya bağlanır: oturum yoksa tap → **kayıt yazılmadan** aktivasyon
  sayfası.
- **WiFi adımı (Q14):** aktivasyon akışında çalışandan mekânın WiFi'ına bağlanması
  istenir (ağ adı lokasyon kaydından gösterilir), "neden" tek cümleyle açıklanır.
  Atlanabilir — zorunlu tutulmaz — ama atlandığında sonraki tap'ler GPS yoluna
  düşer. IP kanıtının (50 puan) pratikte var olması buna bağlı.

- **GDPR Art. 13 aydınlatma metni burada gösterilir** — çalışanın veri işlemeyi
  ilk gördüğü yer. İçerik: tap anında GPS ve kaynak IP okunduğu, mesai kaydının
  ne kadar saklandığı (Q13), veri sorumlusunun işveren olduğu, biyometrik veri
  **toplanmadığı**. Aktivasyon bu ekran onaylanmadan tamamlanmaz.

**Tuzaklar.**
- Davet e-postası Q02'ye bağlı. Q02 cevapsızsa akışı kodla ama gönderimi arayüz
  ardına al (kodu panelde göster) — görevi tamamen bloklama.
- **Ama "kodu panelde göster" kalıcı çözüm DEĞİL** (Y-D). Davet kodunu üreten ve
  gören kişi ile bordroyu şişirmekte en güçlü teşviki olan kişi aynı: müdür sahte
  bir profil açar, kodu ekrandan okur, kendi telefonunun ikinci tarayıcı
  profilinde aktive eder → her gün trust 100, gerçek çalışandan ayırt edilemez.
  Q02 çözülünce kod **çalışanın kendi kanalına** (e-posta/SMS) gider; bu
  zorunludur, opsiyon değil. O zamana kadar risk ADR 0005'te kayıtlı ve tespit
  sinyali M6-11'de.
- Kod entropisi ve deneme sınırı belirtilmeli: ya ≥128 bit, ya da insan okuyacak
  kısa kod ise **katı kilitleme** (IP+kod bazlı, kısa TTL, başarısız deneme
  `audit_log`'a). M5-03'ün tap oran sınırı bilinçli olarak geniş — `/api/activate`
  onun kapsamına **girmez**, kendi dar sınırı olur. 10⁶ uzay + sınırsız deneme +
  15 bekleyen davet = saniyeler içinde ele geçirilen bir kimlik.

---

## M5-03 — Middleware: gerçek IP, tenant, oran sınırı

- **Bağımlılık:** M1-07 · M5-01
- **Kırmızı çizgi:** §4.5
- **Commit:** `feat(httpx): add real-ip, tenant and rate-limit middleware`

**Amaç.** İstek sınırında bağlamı kurmak: kim, hangi tenant, hangi IP.

**Kabul kriterleri.**
- Gerçek IP yalnızca `cfg.TrustedProxies` içindeki hop'lardan gelen
  `X-Forwarded-For` ile çözülüyor; dışarıdan gelen başlık **yok sayılıyor**
  (aksi halde *proof of place* uydurulabilir).
- Tenant bağlamı `WithTenant` üzerinden; handler'da elle `SET` yok.
- Oran sınırı: tap uçları için IP + oturum bazlı. Sınır, meşru bir tap'in **asla
  değemeyeceği** kadar geniş olmalı; aşan istekler `429` alır ve `audit_log`'a
  yazılır — sessizce düşürülmez. (§4.6 "kayıt asla kaybolmaz" ile çelişmemesi
  için: sınır bir kötüye kullanım kalkanıdır, meşru mesaiyi eleyen bir filtre
  değil. Debounce zaten guardrail'de.)
- Panik kurtarma zaten var; 500 dönerken tap kaydı kaybolmuyor mu — M5-05'te
  ayrıca ele alınır.

**Tuzaklar.**
- `middleware.RealIP` chi'de başlığa **koşulsuz** güvenir. Tappa'da bu yeterli
  değil; `TrustedProxies` ile sınırlı kendi middleware'ini yaz veya RealIP'i
  yalnız güvenli dağıtımda kullan. Bu, denetimde R5 kapsamına girer.

---

## M5-04 — `GET /t`: tap sayfası

- **Bağımlılık:** M2-07 · M5-01 · M5-03
- **Araç:** skill `tappa-brand`
- **Commit:** `feat(tap): serve the employee tap page`

**Amaç.** Plakete dokunulduğunda açılan tek ekran.

**Kabul kriterleri.**
- "Hello Maria" + lokasyon adı + **tek** buton (≥64px yükseklik, ıslak/eldivenli
  parmakla basılabilir).
- Menü, sekme, ayar **yok**.
- Sunucu tarafında SUN ön-doğrulaması yapılır; sayfa açılırken sayaç **ilerletilmez**
  (ilerletme `POST /api/checkin` anında — aksi halde sayfayı açıp basmayan kullanıcı
  sayacı harcar).
- GPS yalnızca butona basıldığında `getCurrentPosition` ile okunur —
  `watchPosition` **yasak** (§4.2).
- Oturum yoksa aktivasyon sayfasına yönlendirir, **kayıt yazmaz**.
- Fontlar `web/static/fonts/` altından self-host; Google Fonts'a runtime bağlantı yok.

**Tuzaklar.**
- Sayfada `ctr`/`cmac` değerlerini JS'ye veya DOM'a gereksiz yere gömme; POST'a
  sunucu tarafında imzalı bir bağlam taşımak daha temiz.
- Karanlık tema yok (şimdilik) — yarım iş yapma.

---

## M5-05 — `POST /api/checkin`: orkestrasyon

- **Bağımlılık:** M2-07 · M4-07 · M5-04
- **Kırmızı çizgi:** §4.3 · §4.4 · §4.6 — **milestone'un en kritik görevi**
- **Commit:** `feat(tap): decide and record check-in`

**Amaç.** Dört kanıtı toplayıp karara bağlamak ve kaydı yazmak.

**Akış.** parse → `sun.Verify` → bağlam topla (oturum, IP, GPS, vardiya, son
işlemler) → `tap.Decide` → **kaydı yaz** → yanıt üret. Handler'da iş kuralı ve
ham SQL **yok** (§3).

**Kabul kriterleri.**
- Karar ne olursa olsun (`ok`/`flag`/`reject`/`ignored`) **kayıt yazılır** —
  tek istisna §5 satır 3 (oturum yok).
- Hata yolunda kayıt kaybı yok: DB yazımı başarısızsa istemciye "Try again"
  dönülür ve olay log'lanır; sessiz yutma yok (§7).
- Sayaç ilerletme CMAC doğrulamasından **sonra**, tek atomik ifadede.
- Yanıt DTO'su `internal/handler`'da; `internal/store` tipleri API'ye sızmıyor.
- Log'da: oturum token'ı, CMAC, AES anahtarı, davet kodu, **tam GPS** yok (§7).
- Deaktive çalışan denemesi → `reject` + güvenlik uyarısı + audit izi.
- **`channel='manual'` yazımında `entered_by` ZORUNLU** (M4-06'dan devredildi):
  boşsa yazma yolunda **hata** — sessiz kabul yok. `tap.Decide` saf ve imzası
  sabit olduğu için (hata döndüremez) bu doğrulama burada, handler sınırında yapılır
  (§7); `entered_by` bir köken alanıdır, karar girdisi değil. Manuel giriş UI'ı ve
  otomatik doldurma M6-04.

- **`occurred_at` istemciden geliyorsa guardrail'e tabidir.** `sys:occurred-at-bound`
  ([M3-05](m3-policy-motoru.md)): gelecek zaman → `deny`; `created_at − occurred_at`
  sapması üst sınırı (0–72 sa) aştıysa → `deny`. Sınır **kapatılamaz**, yalnız
  aralık içinde ayarlanır.
- `tap:queued` **istemci beyanından değil**, sunucudaki `occurredAtSkewSeconds`
  farkından türetilir.

**Tuzaklar.**
- `flag` kararını sonradan `ok`'a çeviren bir UPDATE yazma — onay ayrı tabloya
  yazılır (§4.3, Q20, M6-04).
- Çevrimdışı kuyruktan gelen geç POST'lar: `occurred_at` istemciden gelir,
  `created_at` sunucudan. İmzalı `ctr` geç senkronda da geçerlidir. Kuyruk
  istemcisi M9-01'de; sunucu tarafı bu ayrımı **şimdiden** doğru kurmalı.
- **Ama sınırsız bırakma (K1).** Zaman dört kanıtın hiçbirinde yok: SUN
  payload'ında zaman yok, IP/GPS/oturum zamansız. Yani `occurred_at`, mesai
  kaydının **en önemli alanı** olmasına rağmen tek doğrulanmamış istemci
  girdisidir. Çalışan 09:00'da gerçekten tap yapar (SUN ✓, IP ✓, tazelik ✓) ama
  gövdeye `occurred_at=07:00` yazar → 2 saat sahte mesai. Sınırı yalnız
  `base:queued-window`'a bırakmak yetmez: o bir **baseline**'dır, tenant
  kapatabilir. Üst sınır guardrail'de olmak zorunda.

---

## M5-06 — Onay ekranı ve marka mesajları

- **Bağımlılık:** M5-05
- **Araç:** skill `tappa-brand`
- **Commit:** `feat(tap): add confirmation screen with tenant messages`

**Amaç.** İşlem sonrası ekran.

**Kabul kriterleri.**
- Başarılıda **buton yok**: *"All done — you can close this page."*
- Başarısızda **"Try again" var**.
- Tenant'a özel mesaj gösteriliyor (KF/KM metinleri skill'de; panelden
  düzenlenebilirlik M9-04).
- Durum rengi sabit eşlemeye uyuyor: `ok → tappa-green`, `flag → saffron`,
  `reject → tomato`, `ignored → line`.
- Durum **yalnız renkle** anlatılmıyor; damga metni de var (erişilebilirlik).
- Saat ve süre **mono** (IBM Plex Mono).

**Tuzaklar.**
- "Bir de şunu ekleyelim" baskısına direnç: bu ekran ürünün en sade parçası ve
  öyle kalmalı. Değişiklik önerisi → önce sor.

---

## M5-07 — Mini tur ve practice tap

- **Bağımlılık:** M5-02 · M5-06
- **Commit:** `feat(activation): add three-slide tour and practice tap`

**Amaç.** Operasyonel onboarding'i sıfırlamak: eğitim kayıt anında tamamlanır.

**Kabul kriterleri.**
- Aktivasyondan hemen sonra 3 slayt: "Tap the plaque" → "One button" →
  "First tap is practice".
- İlk check-in `practice = true`, **TRAINING** damgası (ink üstüne kesikli çerçeve).
- Practice kaydı çalışılan saate **asla** sayılmıyor ve yön zincirine girmiyor
  (M4-06 ile tutarlı).
- Tur atlanabiliyor ama practice bayrağı yine ilk kayda düşüyor.

---

## M5-08 — QR kanalı

- **Bağımlılık:** M5-05
- **Commit:** `feat(tap): support the qr fallback channel`

**Amaç.** NFC'siz/kapalı telefonlar için yedek yol.

**Kabul kriterleri.**
- QR statik → `ctr`/`cmac` yok → `sun_valid = false`, `channel = 'qr'`.
- **Bu kanalda IP zorunlu; GPS tek başına yetmez → `flag`** (Q15 kararı,
  `base:qr-requires-ip`). Gerekçe: QR fotoğraflanır, süresiz geçerlidir ve
  hiçbir fiziksel dokunuş kanıtı taşımaz — tek gerçek kanıt mekânın ağında olmak.
- QR asla NFC ile aynı trust seviyesine çıkmıyor.
- Plaket baskısında NFC + QR birlikte — tasarım notu `tappa-brand` ile uyumlu.
- **QR bazı çalışanlar için kalıcı yoldur, yedek değil.** iPhone X ve öncesi
  arka planda NFC etiketi okuyamaz (arka plan okuma iPhone XS/XR ve sonrası) —
  o telefonlardaki çalışanlar için URL hiç açılmaz. Q15 gereği QR'da IP zorunlu
  olduğundan, bu çalışanlar mekânın WiFi'ına bağlanmazsa **her gün** `flag`
  üretir. Pilot öncesi telefon envanteri çıkarılır ([M8-07](m8-deploy-pilot.md));
  oran yüksekse Q15 varsayılanı tenant bazında yeniden değerlendirilir.

---

## M5-09 — Uçtan uca entegrasyon testi ve "bir günü simüle et"

- **Bağımlılık:** M5-05 … M5-08 · M1-10
- **Kırmızı çizgi:** §8
- **Commit:** `test(tap): add end-to-end flow and full-day simulation`

**Amaç.** Gerçek Postgres'e karşı, gerçek HTTP üzerinden akışı kanıtlamak; ve
dashboard'un üstünde duracağı gerçekçi bir günü üretmek.

**Kabul kriterleri.**
- Aktivasyon → practice tap → normal in → out zinciri uçtan uca yeşil.
- KF St Julians "bir günü simüle et": 10 çalışan, ~21 işlem, skill `tappa-seed`
  tablosundaki **her senaryo** dahil (geç kalma, çift tap, ardışık farklı kişiler,
  mobil veri, flag, deaktive deneme, retired plaket, QR, practice, manuel,
  Rusty Bar gece vardiyası).
- Üretilen kayıtlar **karar motorunun çıktısıyla** oluşuyor — `verdict`, `trust`,
  `ip_match` elle uydurulmuyor. Tutarsız veri hatalı motoru doğru gösterir.
- `-race` ile temiz.
- Milestone kapanışı: agent `tappa-security-auditor` tam denetim, `make audit` temiz.

---

## M5-10 — Tap tazelik penceresi (URL biriktirmeye karşı)

- **Bağımlılık:** M5-04 · M5-05 · M3-05 (guardrail)
- **Kırmızı çizgi:** §4.4 (replay koruması) · §4.6
- **Commit:** `feat(tap): bind a tap url to a freshness window`

**Amaç.** Açılmış bir tap sayfasının süresiz geçerli kalmamasını sağlamak.

> ⚠️ **Bu görev A1'i (URL biriktirme) ÇÖZMEZ.** Dış denetimde ortaya çıktı:
> pencere `GET /t` anından başlıyor, yani "fiziksel dokunuştan beri geçen süreyi"
> değil "sayfayı açtıktan sonra butona basana kadar geçen süreyi" ölçüyor.
> Saldırgan telefonu **uçak moduna** alıp plakete 10 kez dokunur — çip her
> okumada `ctr`'ı artırır, URL adres çubuğunda görünür, ama istek sunucuya hiç
> ulaşmaz. Ertesi gün evden URL'yi açar → `GET` *o an* kaydedilir → 5 saniye
> sonra POST → pencere içinde → geçer.
> A1'in gerçek karşılığı **`tap:ctrGap` + `base:ctr-gap-review`** politikasıdır
> (Q21) ve tam çözüm değildir — ADR 0005'te kabul edilen risktir.

**Neden yine de gerekli.** [M5-04](m5-tap-akisi.md) bilinçli olarak sayacı sayfa
açılışında değil POST anında ilerletir (sayfayı açıp basmayan kullanıcı sayacı
harcamasın). Bu doğru UX kararının bedeli: açılmış bir sayfa süresiz geçerli
kalır. Pencere bu bedeli sınırlar — plakete dokunup sayfayı açık bırakan,
saatler sonra (belki başka bir yerden) basan kullanıcıyı durdurur.

**Tasarım.**
1. `GET /t` anında `(tag_uid, ctr, first_seen_at, source_ip)` kaydedilir.
   *(Alan adı bilerek `request_fingerprint` değil: `scripts/redline-check.sh` R1
   `fingerprint` desenini FAIL olarak tarar ve §4.1'in dili tarayıcı parmak izine
   kapalıdır. Tutulan şey yalnız kaynak IP, yalnız anomali metriği için.)*
   Bu tablo **kendi migration'ıyla gelir**: `tap_page_views(tag_uid, ctr,
   first_seen_at, source_ip)` + `tenant_id` + RLS beşlisi + saklama süresi
   (eski satırlar temizlenir — kimliksiz herkes `GET /t` açabildiği için
   temizliksiz tablo ucuz bir disk doldurma vektörüdür).
2. `POST /api/checkin` yalnız `first_seen_at + pencere` içindeyse kabul edilir.
3. Pencere **sınırlı parametre**: guardrail kapatılamaz, süre tenant tarafından
   **1–15 dk** arası seçilir (varsayılan 3 dk).
4. Kayıt: pencere aşımı `sys:tap-freshness` ile `deny` → **kayıt yine yazılır**
   (§4.6), verdict `reject`, gerekçe açık.

**Kabul kriterleri.**
- Pencere içinde normal tap etkilenmiyor (kullanıcı butona basana kadar geçen
  birkaç saniye).
- Pencere dışı POST → `reject` + gerekçe `sys:tap-freshness`.
- **Sıra senaryosu korunuyor:** A dokunur, arkasından B dokunur, A butona basar
  → A hâlâ geçerli. (Reddedilen alternatif: `GET`'te `seen_ctr` ilerletmek —
  daha sıkı ama arkadaki kişinin dokunuşu öndekinin sayfasını geçersizleştirir
  ve §5'in "ardışık farklı kişiler hepsi `ok`" gereksinimini kırar.)
- Çevrimdışı kuyruk (M9-01) ile uyumlu: kuyruğa alınan istek `queued` damgası ve
  `base:queued-window` politikasıyla ayrı değerlendirilir — **ama üst sınır
  `sys:occurred-at-bound` guardrail'indedir**, kapatılamaz.
- **Satır yalnız CMAC doğrulandıktan sonra yazılır.** Aksi hâlde saldırgan
  `GET /t?tag=<UID>&ctr=<gelecek değerler>&cmac=<çöp>` ile gelecek sayaçlar için
  eski `first_seen_at` damgalı satırlar yaratır; ertesi gün o plakete dokunan
  meşru kullanıcıların POST'u "pencere dışında" bulunup **reject** alır. UID gizli
  değildir (URL'de açık). Aynı sebeple POST **en yeni** GET kaydına bakar.
- Sayaç boşluğu (`tap:ctrGap`) hesaplanıp kayda yazılıyor ve
  `base:ctr-gap-review` politikasına besleniyor (Q21). M2-06'daki "> 1000 → flag"
  eşiği kaldırıldı: biriktirme ~10'luk boşluk üretir, 1000 onu hiç görmez.
- Tespit sinyalleri M6-11'de: **POST'suz `GET /t` sayısı** *ve* — asıl önemlisi —
  **`ctr` boşlukları**. İlki uçak modu senaryosunda sıfır kalır, ikincisi kalmaz.

**Tuzaklar.**
- Pencereyi çok kısa tutmak (< 1 dk) meşru kullanıcıyı düşürür: telefon kilidi
  açılır, sayfa yüklenir, kullanıcı ekranı okur. 3 dk makul başlangıç.
- `first_seen_at` sunucu saatidir; istemciden gelen zamana **asla** güvenilmez.
- Bu görevi "A1 kapandı" diye işaretleme. Kapanmadı; sinyal ekledi.
