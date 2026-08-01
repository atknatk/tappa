# M6 — Admin dashboard

**Amaç.** Müdürün gördüğü ürün: beş sekme — Transactions · Employees ·
Locations & Wall Tags · Reports · Policies. Marka diline sadık, docket motifiyle.

**Bittiğinde:** KF ve KM verisiyle dolu, gerçek kararlar üzerinde çalışan bir
panel; FLAGGED kuyruğu işliyor, CSV dışa aktarım var.

**Ana araç:** skill `tappa-brand` — her ekran görevinde önce oku.

> İşlem kayıtları **docket** (mutfak adisyonu) olarak gösterilir, düz tablo
> satırı olarak değil. Bu ürünün tanınma işareti.

---

## M6-01 — Admin kimlik doğrulama

- **Bağımlılık:** M1-04 · Q03
- **Kırmızı çizgi:** §4.5 · §4.7
- **Commit:** `feat(auth): add admin login for the dashboard`

**Amaç.** Panele giriş. Çalışan oturumundan **ayrı** bir oturum türü.

**Kabul kriterleri.**
- Şifre hash'i Q03 kararına göre (bcrypt veya argon2id); düz veya hızlı hash yok.
- Admin oturumu çalışan oturumundan ayrı çerez/tablo; bir çalışan çerezi panele
  erişemiyor ve tersi.
- Oturum tenant'a bağlı; giriş sonrası tüm sorgular o tenant bağlamında.
- Başarısız giriş denemeleri oran sınırlı ve `audit_log`'a yazılıyor.
- Rol ayrımı (owner/manager) için alan var; yetkilendirme MVP'de kaba olabilir
  ama şema hazırlıklı.

**Tuzaklar.**
- Admin'in çapraz-tenant görmesi **yok**. "Destek için" bir bypass eklemek §4.5
  ihlalidir; gerekirse ayrı ADR.

---

## M6-02 — Dashboard iskeleti ve docket bileşenleri

- **Bağımlılık:** M6-01
- **Araç:** skill `tappa-brand`
- **Commit:** `feat(ui): add dashboard layout and docket components`

**Amaç.** Tekrar eden desenleri bileşen olarak bir kez yazmak.

**Dokunulacak dosyalar.** `web/templates/layouts/`, `web/templates/components/`,
`web/static/css/input.css`

**Kabul kriterleri.**
- Bileşenler: `docket`, `stamp` (approved/flagged/rejected/training), buton, boş
  durum, sekme navigasyonu, filtre çubuğu.
- Semantik sınıflar `input.css` içinde `@layer components` (`.docket`, `.stamp`,
  `.stamp--approved`).
- Perforasyon saf CSS `radial-gradient` — görsel dosya **yok**.
- Kaşe damgası hafif eğik (`-rotate-6`…`-rotate-12`), çift çerçeve; **kelime
  `ink`, durum rengi çerçevede** — 2px kenarlık + 1px iç halka + `bg-<token>/10`
  zemin. *(Güncellendi 2026-08-01, kullanıcı kararı. Bu satır **"yarı saydam"**
  diyordu; `opacity-80` o gün `.stamp`'ten **kaldırıldı**, çünkü grup opaklığı
  rengi taşıyan **çerçeveyi** soluklaştırıyordu: saffron kenarlık `paper` üstünde
  2,62:1 → **2,14:1**, `line` 1,52:1 → **1,39:1**. Kelime ink olduğu için beş
  damganın beşi de AA'yı geçiyor (en kötü 13,27:1). Ölçüm ve tam gerekçe: M5-06
  kartı md.7 + skill `tappa-brand`. Eski anatomiyi geri koyma.)*
- Her sayı **mono**; palet dışı renk yok; gradient yok.
- Dokunma hedefi ≥44px, kontrast AA, `prefers-reduced-motion` saygılı.
- `make gen` sonrası `*_templ.go` commit edildi.

---

## M6-03 — Transactions sekmesi

- **Bağımlılık:** M6-02 · M5-09
- **Commit:** `feat(dashboard): add transactions view`

**Amaç.** Günün işlemlerini docket kartları olarak göstermek.

**Kabul kriterleri.**
- Filtreler: tarih, lokasyon, departman, çalışan, verdict, channel.
- Her kart: lokasyon, isim, in/out, saat, trust, IP/GPS işaretleri, tag UID + ctr,
  kaşe damgası.
- `manual` ve `practice` kayıtlar görsel olarak ayırt ediliyor.
- Sayfalama/lazy yükleme HTMX ile; istemci state'i yok.
- Sorgular tenant filtreli ve indeksli (`(tenant_id, occurred_at DESC)`).

---

## M6-04 — FLAGGED onay kuyruğu

- **Bağımlılık:** M6-03
- **Kırmızı çizgi:** §4.3 — **bu sekmenin en kritik kısmı**
- **Commit:** `feat(dashboard): add flagged approval queue`

**Amaç.** Kanıt yetersizliğiyle alınan kayıtları müdürün karara bağlaması.

**Kabul kriterleri.**
- Onay/ret **`transactions`'a hiç dokunmaz** (Q20). Karar `transaction_reviews`
  tablosuna yazılır (append-only) + `audit_log`'a `actor_id`, `action`, `target`,
  `at` düşülür. Liste ve raporlar son onayı JOIN ile okur.
- Orijinal `flag` kaydı raporlarda ve geçmişte görünmeye devam eder.
- Kuyruk boşsa anlamlı boş durum; sayaç navigasyonda görünüyor.
- Toplu onay varsa her satır için ayrı audit kaydı.
- `tappa_app` zaten `transactions` üzerinde UPDATE yetkisine sahip değil (M1-06)
  — bu sekme o kısıta **çarpmamalı**, yani doğru tasarlanmış olmalı.

**Tuzaklar.**
- "Sadece verdict sütununu güncelleyelim" en kolay ve en yanlış yol. Mesai kaydı
  hukuki delil olabilir; geçmiş değişmez.

---

## M6-05 — Employees sekmesi

- **Bağımlılık:** M6-02 · M5-02
- **Commit:** `feat(dashboard): add employees management`

**Kabul kriterleri.**
- Liste: isim, lokasyon/departman, durum (`invited|active|deactivated`), oturum
  durumu (aktif cihaz var mı, son kullanım).
- Aksiyonlar: davet et, yeniden davet, deaktive et, lokasyon/departman değiştir.
- **Deaktive → oturum o saniye ölür** (`revoked_at`), sonraki tap `reject`.
- Her aksiyon `audit_log`'a yazılıyor.
- Davet kodu ekranda bir kez gösteriliyor, log'a yazılmıyor.

---

## M6-06 — Locations & Wall Tags sekmesi

- **Bağımlılık:** M6-02 · M1-05
- **Kırmızı çizgi:** §4.7
- **Commit:** `feat(dashboard): add locations and wall tags`

**Kabul kriterleri.**
- Lokasyon CRUD: ad, statik IP listesi, GPS, vardiya, `overnight`.
- Plaket listesi: UID, durum, bağlı lokasyon, `last_ctr`, son görülme.
- **Replace tag** akışı: yeni UID kaydedilir, eski `retired` + `retired_at` +
  `replaced_by`; eski etikete tap → `reject`. Toplam süre 30 saniyeyi geçmemeli.
- Tag geçmişi görünür (audit); eski satır **silinmiyor**.
- AES anahtarı ekranda **hiç** gösterilmiyor; yalnızca "encoded/pending" durumu.
- Departman yönetimi (KM için) aynı sekmede veya ayrı; vardiya alanları dahil.

---

## M6-07 — Reports ve CSV export

- **Bağımlılık:** M6-03 · M4-05
- **Commit:** `feat(dashboard): add reports and csv export`

**Kabul kriterleri.**
- Günlük/haftalık çalışılan saat, çalışan ve lokasyon kırılımı.
- Geç kalmalar çalışanın **kendi** vardiyasına göre (M4-05).
- `practice = true` kayıtlar saate **dahil değil**; ayrı gösteriliyor.
- `manual` kayıtlar raporda **ayrı işaretli** (handoff §6).
- **Açık kalan giriş (çıkışsız) — Q18 kararı:** sistem otomatik çıkış **üretmez**.
  Bu kayıtlar saat toplamına **girmez**, ayrı bir "open / needs action" bölümünde
  listelenir ve rapor toplamın **eksik olduğunu açıkça söyler**. Sessizce 0 saat
  saymak bordroyu eksik çıkarır. Müdür manuel çıkış girer (M6-08).
  - ⚠️ **`practice = true` girişler bu bölüme GİRMEZ** (eklendi 2026-08-01, M5-07
    denetimi). Practice tap bir `type='in'` satırıdır ve SQL anlamında **kapanmamış**
    görünür — `GetLastOpenTransaction`'ın `NOT EXISTS`'i onu ancak bir `out` gelince
    eler, oysa deneme tap'inin ardından hiçbir zaman bir `out` gelmez. Bu kriter
    yazıldığında practice istisnası yalnız **saat** satırında (üç satır yukarıda)
    yazılıydı; ikisini ayrı bırakmak, M5-07'nin turunun *"ilk tap'in deneme"*
    vaadini müdürün **"eylem gerekiyor"** kuyruğunda anomali olarak gösterirdi —
    yani her yeni çalışan, ilk gününde, kapatılması istenen bir kayıt üretirdi.
    Karar motoru tarafında zaten böyle: practice kaydı yön zincirini açık tutmaz
    (`checkin.go` `gather` + `tap/decide.go` `resolveDirection`, ikisi de testle
    pinli). Rapor sorgusu **aynı istisnayı taşımak zorunda**.
- CSV: UTF-8, mono hizalı başlık, saatler UTC **ve** yerel — hangisi olduğu
  başlıkta açık. Excel'in tarih bozmaması için ISO 8601.
- Saat hesabı `numeric`/`Duration` ile — **float yok** (§6).

---

## M6-08 — Manuel kayıt girişi

- **Bağımlılık:** M6-03
- **Kırmızı çizgi:** §4.3 · §4.6
- **Commit:** `feat(dashboard): add manual record entry`

**Amaç.** Telefonsuz çalışan için vardiya amirinin kayıt girmesi.

**Kabul kriterleri.**
- `channel = 'manual'`, `entered_by` otomatik doldurulur (giriş yapan admin).
- `sun_valid = false`, trust taban puanı; raporlarda ayrı görünüyor.
- Geçmişe dönük kayıt mümkün ama `created_at` gerçek yazım anını gösteriyor.
- `audit_log` kaydı zorunlu.
- Var olan bir kaydı **düzeltmek** = yeni satır + audit; UPDATE yok.

---

## M6-09 — Policy yönetim ekranı

- **Bağımlılık:** M6-02 · M3-06
- **Kırmızı çizgi:** §4 (guardrail'ler ekranda **görünür ama kapatılamaz**)
- **Araç:** skill `tappa-brand`
- **Commit:** `feat(dashboard): add policy management screen`

**Amaç.** Müşterinin kendi kurallarını görmesi, açıp kapatması ve
parametrelerini ayarlaması. Policy motorunun kullanıcıya bakan yüzü.

> **v1 kapsamı dar (Q22):** yalnız **form** arayüzü. Ham JSON editörü
> ([M9-07](m9-sonrasi.md)) ve simülatör ([M9-06](m9-sonrasi.md)) pilot sonrasına
> ertelendi. Tenant v1'de baseline politikalarını açar/kapatır ve sınırlı
> parametreleri ayarlar; sıfırdan belge yazamaz.

**Kabul kriterleri.**
- Üç katman **ayrı ayrı ve açıkça** gösteriliyor:
  - **Guardrail** — kilit ikonu, "Tappa güvencesi, kapatılamaz", gerekçe metni
    (hangi kırmızı çizgi). Kapatma kontrolü ekranda **yok**, sadece disabled da
    değil — hiç yok.
  - **Baseline** — aç/kapa anahtarı + "kendi sürümünü oluştur".
  - **Tenant** — tam CRUD.
- Sık kullanılan politikalar için **form** (JSON yazmak zorunda değil):
  "QR ile giriş: IP zorunlu / GPS yeterli / her zaman onaya düşsün" gibi.
- Guardrail'lerin **sırası** da gösteriliyor (M3-05'teki 1→10) — müdür bir
  kararın neden o guardrail'e takıldığını görebilsin.
- Yetkilendirme politikaları (`policy:edit`, `report:export`, `tap:approve`)
  ayrı bir bölümde; varsayılanın **fail-closed** olduğu ekranda yazıyor.
- Kapsam bağlama: politika tüm tenant'a mı, belirli lokasyona/departmana mı
  (`resource`). KF'nin Rusty Bar'ı ile HQ'su aynı kuralı paylaşmak zorunda değil.
- **Sürüm geçmişi**: kim, ne zaman, ne değiştirdi; eski sürüme bakılabiliyor.
  Düzenleme yeni sürüm üretiyor, üzerine yazmıyor.
- Her değişiklik `audit_log`'a yazılıyor.
- Sınırlı parametreler (debounce, tazelik penceresi) aralıklarıyla birlikte
  gösteriliyor; aralık dışı değer arayüzde de reddediliyor.

**Tuzaklar.**
- Guardrail'i "kapalı görünen bir anahtar" olarak çizme — müşteri açmayı dener,
  açamaz, güven kaybeder. Kilit + gerekçe doğru dil.
- Politika kaydetmeyi "hemen yürürlüğe girer" yapma; M6-10 simülasyonu önce
  gösterilmeli.

---

## M6-10 — Policy simülatörü — ⛔ ERTELENDİ → [M9-06](m9-sonrasi.md)

> **Q22 kararı:** M3 v1 kapsamı daraltıldı; simülatör pilot sonrasına alındı.
> Bu ID yeniden kullanılmaz, ledger'da `skipped` olarak kalır. Aşağıdaki spec
> M9-06'ya taşındı ve orada **bağlam anlık görüntüsü** gereksinimiyle birlikte
> duruyor — o gereksinim ([M3-07](m3-policy-motoru.md) `policy_context jsonb`)
> **1. günden itibaren** karşılanmalı, yoksa simülatör sonradan da yazılamaz.

- **Bağımlılık:** M6-09 · M3-04
- **Commit:** `feat(dashboard): simulate policy changes against past taps`

**Amaç.** "Bu kuralı açarsam ne değişir?" sorusunu **kayıt değiştirmeden**
cevaplamak.

**Neden.** Mesai kuralı değiştirmek bordroyu etkiler. Müşteri sonucu görmeden
açarsa ya korkup hiç dokunmaz ya da farkında olmadan onay kuyruğunu patlatır.
AWS policy simulator'ın karşılığı.

**Kabul kriterleri.**
- Seçilen tarih aralığındaki gerçek tap'ler **yeniden değerlendiriliyor**
  (kayıtlara **yazılmadan** — `transactions` immutable, §4.3).
- Çıktı: "geçen hafta 214 tap → 6 kayıt `ok`'tan `flag`'e geçerdi" + etkilenen
  docket listesi.
- Simülasyon deterministik (M3-04'teki sıralama garantisi buna dayanıyor).
- Yayına almadan önce simülasyon sonucunu gösteren onay adımı.
- Simülasyon **guardrail'leri de** uyguluyor — müşteri "kapatırsam ne olur"u
  deneyemiyor bile.

---

## M6-11 — Anomali ve kötüye kullanım raporu

- **Bağımlılık:** M6-07 · M5-10
- **Commit:** `feat(dashboard): add anomaly report`

**Amaç.** Müdüre "burada bir tuhaflık var" diyen tek ekran. Önleme değil
**tespit** — planın kabul ettiği risklerin (buddy punching, sahte GPS,
URL biriktirme) görünür kalması buna bağlı.

**Kabul kriterleri.**
- **GPS-only tap oranı** — çalışan ve lokasyon kırılımında. Yüksek oran ya WiFi
  sorunu ya kötüye kullanım; ikisi de bilinmeli.
- **`ctr` boşlukları** (`tap:ctrGap > 0`) — URL biriktirmenin **tek gerçek izi**
  (A1/Q21). Çalışan ve plaket kırılımında.
- **POST'suz `GET /t` sayısı** — ikincil sinyal. Uçak modu senaryosunda sıfır
  kalır, bu yüzden tek başına yeterli değildir.
- **"IP eşleşti ama GPS uyuşmuyor"** (`tap:gpsConflict`) — mekânda bırakılmış
  proxy sinyali (Y-E). Bu saldırı GPS-only oranında **görünmez**; kayıtları
  tersine "IP ile doğrulanmış" olarak en güvenilir gösterir.
- **Tek cihaz/oturum kaynağından aktive edilmiş birden fazla çalışan** ve
  **hiç çapraz-lokasyon göstermeyen çalışan** — müdürün kimlik basması sinyali (Y-D).
- **Eş-zamanlı tap çiftleri** — her gün saniyeler içinde birlikte tap yapan
  kişiler (buddy punching sinyali). Suçlama değil, bakılacak yer.
- **Çıkışsız açık kayıtlar** ve **çapraz lokasyon tap'leri**.
  - ⚠️ **`practice = true` kayıtlar "çıkışsız açık kayıt" SAYILMAZ** (eklendi
    2026-08-01, M5-07 denetimi) — gerekçesi M6-07'nin aynı maddesinde. Deneme
    tap'i tanım gereği çıkışsızdır; anomali listesine düşerse liste her yeni
    çalışan için bir kez **yanlış pozitif** üretir ve "rapor yorum yapmaz, veri
    gösterir" ilkesi tam da burada yorum yapmış olur.
- **Politika kırılımı**: hangi `sid` kaç kayıt üretti (M3-07 sayesinde makine
  tarafından filtrelenebilir).
- Rapor **yorum yapmaz**, veri gösterir. "Şüpheli çalışan" etiketi yok.

---

## M6-12 — Çalışan sayımı ve fatura taslağı

- **Bağımlılık:** M6-05 · M6-07
- **Commit:** `feat(dashboard): add monthly headcount and invoice draft`

**Amaç.** €1.50/çalışan/ay'ın **ne kadar** olduğunu makine hesaplasın; tahsilat
elle yapılsın.

**Neden.** Denetimde çıktı: tahsilat planın hiçbir yerinde yoktu — görevlerde de,
"MVP kapsamı dışında" listesinde de. Yani bilinçli erteleme değil, boşluk.
Karar (Q24): iki müşteri için **otomatik ödeme MVP dışı**, ama sayım otomatik
olmalı — çalışan sayısı ay içinde değişince faturalanacak rakam tartışmalı hâle
gelmesin.

**Kabul kriterleri.**
- Ay bazında **faturalanabilir çalışan** sayımı: tanım açıkça yazılı (ör. ayın
  herhangi bir gününde `active` olan çalışan), ekranda da görünüyor.
- Aylık fatura taslağı: tenant, dönem, çalışan sayısı, birim fiyat, toplam.
  CSV/PDF dışa aktarımı — gönderim ve tahsilat elle.
- **Founding offer takibi:** ilk 3 ay ücretsiz; 3. ayın sonunda panelde ve
  raporda görünür bir uyarı (aksi hâlde ücretsiz dönem sessizce uzar).
- Sayım geçmişi saklanıyor: geçmiş bir ayın rakamı sonradan yeniden
  hesaplanmıyor, dondurulmuş değer okunuyor (çalışan sonradan silinse bile
  fatura değişmez).
- Fiyat tenant kaydından okunuyor, koda gömülü değil.

**Tuzaklar.**
- Ödeme sağlayıcısı entegrasyonuna girme — Q24 kararı bu değil. Üçüncü müşteriden
  sonra ayrı bir görev olarak değerlendirilir.
