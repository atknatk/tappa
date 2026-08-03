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

> **Kart güncellemesi (2026-08-02) — M6-01 A/B fazına bölündü; A fazı bitti.**
> M5-02'nin kalıbı: **A = veri katmanı**, **B = auth + ekranlar**.
>
> **A fazı teslim edildi** — migration
> [00011](../../db/migrations/00011_add_admin_resolution.sql) (yeni tablo YOK;
> 00006 uygulanmış ve değişmez), `db/queries/admins.sql`, `internal/db/resolve.go`
> içindeki iki elle yazılmış çözümleyici, `internal/db/admins_test.go`.
>
> **Kartın gizli varsayımı çürüdü ve karara bağlandı.** 00006 "resolver YOK, admin
> girişi tenant bağlamı KURULU iken yapılır" diyordu; **hiçbir şey o bağlamı
> kurmuyordu** (e-posta yalnız tenant içinde tekil, `tenants`'ta slug/subdomain
> yok, tek giriş adresi var). **Kullanıcı kararı (2026-08-02): global çözümleme +
> tenant seçici** — tek giriş, tek e-posta, parola doğrulandıktan **sonra** "hangi
> işletme?" ekranı. Gerekçe ve reddedilen alternatif (tenant'ı imzalı çerezde
> taşımak): ADR 0002 → M6-01 güncelleme bloğu.
>
> **B fazının devraldığı sınırlar** (hiçbiri A fazında kapatılamaz):
> 1. **E-posta numaralandırma + zamanlama.** `resolve_admin_by_email` parola
>    olmadan "bu e-posta kayıtlı mı"yı cevaplayabilir. B fazı: üç sonuç için
>    (bilinmeyen e-posta / yanlış parola / devre dışı admin) **aynı** yanıt; 0
>    satırda **kukla bcrypt**; oran sınırı; `audit_log`.
> 2. **🔴 bcrypt AMPLİFİKASYONU — 1. maddeden AYRI bir sınır (DoS, zamanlama
>    değil), ve bu dosyanın en büyük sayısı.** Çözümleyici **N satır** döndürür;
>    B fazı her satırı bcrypt'ler, yani pahalı yarı **çağıranın** tarafındadır.
>    Ölçüldü (bu şemada, transaction içinde ekilip geri alındı): kurbanın adresi
>    **500 tenant**'a ekildiğinde çözümleyici **500 satır**'ı sıcakta
>    **~0,9–1,2 ms**'de (soğukta 2,8 ms) döndürüyor — **darboğaz veritabanı
>    DEĞİL**. cost-10 bir bcrypt karşılaştırması **~60–100 ms** olduğuna göre tek
>    bir **kimliksiz** `POST /login` **~30–50 sn CPU** satın alır: tek istekten
>    **~500×** amplifikasyon. **Bugün sömürülemez** — tenant yaratan bir
>    **uygulama yolu yok**: `db/queries`'te `CreateTenant`/`InsertTenant` sorgusu
>    yok ve `INSERT INTO tenants` yalnız `test/fixtures/seed.sql` ile test
>    yardımcılarında geçiyor (grep'lendi). **M7-02 tam
>    da bunu değiştirir:** kayıt sihirbazı **herkese açık** ve RLS bir tenant'ın
>    **kendi** `admin_users`'ına **istediği e-postayı** yazmasını engellemez →
>    o andan itibaren **satır sayısını, yani sınırı, saldırgan belirler.**
>    Masadaki seçenekler — **kararı B fazı kendi ölçümüyle verecek**, burada
>    verilmedi: tek istekte doğrulanacak aday sayısına **üst sınır** · **ilk
>    eşleşmede durmak** · M7-02'de **e-posta doğrulaması** olmadan admin satırının
>    çözümlenememesi. Üçü de bir giriş hata modunu DoS sınırına takas eder.
>    ⚠️ **Ortadaki seçeneği 6. madde ile BİRLİKTE oku** — yanlış uygulanırsa
>    tam olarak oradaki atlatmayı üretir.
> 3. **bcrypt bağımlılığı ve karşılaştırma B fazınındır** (Q03). Şema KDF-agnostik.
> 4. **Seçici ekranı işletme adını `GetAdminForTenantChoice` ile, tenant bağlamı
>    içinde ve YALNIZCA parola doğrulandıktan sonra okumalı** — çözümleyici tenant
>    adını bilinçli olarak döndürmez (pre-auth bir ad, numaralandırma sinyalidir).
> 5. **`store.AdminUser.PasswordHash` handler'da `%+v`/slog ile loglanmamalı**
>    (state.md'nin M1-11'den devrettiği not). Bu madde A fazında **kısmen**
>    kapandı ve kalanı dürüstçe yazılıyor: `db.ResolvedAdmin.PasswordHash` artık
>    çıplak `string` değil, **redakte eden `db.PasswordHash` tipi**
>    (`internal/db/passwordhash.go` — `session.Token`/`invite.Code` kalıbının
>    üçüncü uygulaması); açık değere tek çıkış `RevealForPasswordComparison()`.
>    **Kapanmayan:** sqlc'nin ürettiği `store.AdminUser.PasswordHash` hâlâ çıplak
>    `string`'dir (üretilen dosya elle düzenlenmez) — o tipi `%+v` ile loglayan
>    handler hâlâ sızdırır. `make audit` R7 desenine `password` **eklendi**, ama
>    ağ dar: yalnız `password` **kelimesi** bir `fmt.`/`log.`/`slog.` çağrısının
>    parantezleri içinde geçerse yakalar; ara değişkene kopyalanan veya nötr adla
>    loglanan bir digest'i yakalamaz. Gerçek koruma **tip düzeyindedir**.
> 6. **🔴🔴 ADAY ↔ PAROLA BAĞI — listenin en ağır maddesi ve tek çapraz-tenant
>    KİMLİK DOĞRULAMA ATLATMASI (§4.5); diğerleri numaralandırma/DoS.**
>    00011'in kanonik numaralandırmasında **"PHASE B OBLIGATION 5"**.
>    Kural: **oturum YALNIZCA hash'i eşleşen adaya verilir; seçici YALNIZCA
>    eşleşen adayları gösterir.**
>    **Veri katmanı bunu uygulayamaz:** çözümleyici bir **küme** döndürür,
>    `CreateAdminSession` parolayı **hiç görmez** — tek istediği
>    `(admin_user_id, tenant_id)` çiftinin kendi içinde tutarlı ve admin'in aktif
>    olması. Seçicinin geri verdiği çift **gerçek DB satırlarından** kuruludur,
>    yani `INSERT … SELECT` gardı da, açık `tenant_id` yüklemi de, RLS
>    `WITH CHECK`'i de memnun olur. Eksik olan bağ SQL'de hiçbir yerde yoktur.
>    **Saldırı (M7-02 sonrası, canlı ölçüldü):** saldırgan herkese açık kayıt
>    sihirbazıyla kendi tenant'ını açar, **kendi** `admin_users`'ına **kurbanın
>    e-postasını** ve **kendi bildiği parolayı** yazar (RLS engellemez — kendi
>    tenant'ı). Giriş: adres **2 aday** çözümler → parola **saldırganın**
>    satırıyla eşleşir → seçici "hangi işletme?" der → saldırgan **kurbanınkini**
>    seçer:
>    ```
>    ### C1: yalnız AttackerCo hash'i doğrulandı; seçici VictimCo'yu sunuyor
>    INSERT … WHERE a.id='aaaa…0001' AND a.tenant_id='1111…1111' AND a.status='active'
>     → INSERT 0 1   (tenant_id = VICTIM)
>    ### C2: sonraki her istek de geçiyor
>     → UPDATE 1 | role=owner | full_name=Victim Owner
>    ```
>    ⚠️ **2. MADDE İLE GERİLİM — yazılı olmadığı için burada yazılıyor.** 2.
>    maddenin *"ilk eşleşmede durmak"* seçeneği bcrypt DoS'unu azaltır; ama
>    **"ilk eşleşmede dur + seçicide tüm adayları göster"** biçiminde
>    uygulanırsa **tam olarak yukarıdaki atlatmadır**. İki madde **birlikte**
>    okunmalıdır: işi ne sınırlarsa sınırlasın, **seçicinin sunduğu küme, hash'i
>    gerçekten doğrulanan kümeden asla geniş olamaz.**
>
> **Kabul kriterlerinin veri-katmanı tarafı karşılandı:** admin oturumu çalışan
> oturumundan ayrı tablo + ayrı çözümleyici (test:
> `TestAdminAndEmployeeSessionsDoNotOverlap`); oturum tenant'a bağlı; rol alanı
> hazır (`TouchAdminSession` `role`'ü de döndürür). **Karşılanmayan (B fazı):**
> şifre hash'i, oran sınırı, `audit_log` yazımı.
>
> **Fazladan gelen (kart istemiyordu, ölçümle gerekçelendirildi):**
> `admin_sessions` üzerinde **sütun-düzeyi UPDATE** + **monotonluk trigger'ı**.
> 00011 öncesi `tappa_app` canlı olarak (a) `revoked_at`'i NULL'a çekebiliyor,
> (b) bir **manager'ın oturumunu owner'a** yönlendirebiliyor (yetki yükseltme),
> (c) `token_hash`'i yeniden yazabiliyordu (oturum ele geçirme). (b) ve (c) yetki
> düzeyinde, (a) trigger'la kapatıldı — trigger `tappa_owner`'ı da bağlar.
>
> **⚠️ Kapanan YOL'dur, YETENEK değil — cümle düzeltildi (2026-08-03).**
> `admin_users` **tablo-geneli UPDATE** grant'ini korur (00011 bunu bilerek yapar
> ve gerekçesi doğru: o tablonun neredeyse her sütunu meşru olarak yazılabilir,
> sütun-kapsamlamak tabloyu saymaktan ibaret olurdu). Sonuç: kapatılan üç etkiden
> **ikisi bir tablo öteden** hâlâ ulaşılabilir. Canlı ölçüldü (`tappa_app`, kendi
> tenant'ında): `SET status='active'` → **UPDATE 1** (devre-dışı bırakma kill
> switch'i geri alındı) · `SET role='owner'` → **UPDATE 1** (kapatılan yetki
> yükseltmesinin **aynısı**) · `SET password_hash=…` → **UPDATE 1**
> (kapatılandan **daha güçlü**: iptali de aşar). **Devir: M6-05** (panel tarafı
> admin yönetimi) **ve M7-04** (parola sıfırlama) — meşru UPDATE'leri onlar yazar,
> `admin_users`'ın sütun grant'i / rol gardı / audit trigger'ı hak edip etmediğine
> onlar karar verir. O zamana kadar koruma "bu repoda öyle bir sorgu yok",
> yani **disiplin**.
>
> **⚠️ `revoked_at` "panelin tek anlık kill switch'i" DEĞİL — düzeltildi.**
> `admin_users.status='disabled'` **daha güçlüdür**: `TouchAdminSession`
> `admin_users`'a join'lediği için **tek satır değişimi** o admin'in **tüm**
> oturumlarını (mevcut + gelecek) öldürür, `revoked_at` ise satır başına birini.
> Ve **güçlü olanın hiçbir monotonluk koruması yok** — `SET status='active'`
> hepsini geri getirir (UPDATE 1, ölçüldü). Trigger **zayıf olanı** sertleştirir;
> yine de değerli (o alan aynı zamanda **denetim kaydıdır**), ama "kill switch
> artık monoton" diye okunamaz.
>
> **⚠️ Sınır: trigger YANLIŞ ilk damgayı da dondurur.** Geçişi kısıtlar, **değeri**
> değil: `SET revoked_at='1970-01-01'` → **UPDATE 1**, ardından düzeltme →
> `ERROR: revocation is monotonic`. `CHECK (revoked_at IS NULL OR revoked_at >=
> created_at)` **yok**. Bugün böyle yazan sorgu yok (hepsi
> `COALESCE(revoked_at, now())` / `now()`), yani sınır — ama denetim cevabının
> **kalıcı olarak** yanlış kalabileceği tek yol budur.
>
> **⚠️ Sınır (M7-04'e devir): iptal sorgusunda eşzamanlılık tuzağı — fail-OPEN.**
> Sevk edilen iki sorgu **güvenli** ve bu ölçüldü: T1 satır kilidini tutarken
> T2'nin `COALESCE`'i T1'in commit'lediği satıra göre **yeniden değerlendiriliyor**,
> aynı değer yazılıyor, `IS DISTINCT FROM` yanlış, trigger ateşlenmiyor. Güvenlik
> `COALESCE` ile `revoked_at IS NULL` gardından gelir, **trigger'ın hoşgörüsünden
> değil**. Gardsız bir `SET revoked_at = now()` iki eşzamanlı yazıcıda
> `ERROR: revocation is monotonic` alır → **transaction geri alınır** → "her
> yerden çıkış yap" **başarısız raporlar, oturum yaşamaya devam eder**. 00011
> trigger'ı `sessions` üzerinde yeniden kullanmaya **açıkça davet ettiği** için bu
> kısıt davetin yanına yazıldı. Kural: `revoked_at`'e her yazım `COALESCE` veya
> `IS NULL` yüklemi taşır.
>
> **⚠️ Seçicinin İKİNCİ O(N)'i (B fazının notuna):** `GetAdminForTenantChoice`
> aday **başına bir tenant-kapsamlı transaction** koşar (tek ifade iki tenant
> bağlamını kapsayamaz) — 500 adayda 500 transaction. Kimlik **doğrulandıktan
> sonra** koştuğu için risk düşük; ama bcrypt döngüsü sınırlandıktan sonra giriş
> yolunda **kalan tek O(N)** budur ve maliyet **veritabanı** tarafındadır. Doğal
> çözüm 6. maddeden geliyor: seçici yalnız **hash'i doğrulanan** adayları görürse
> bu döngü de yan etki olarak sınırlanır.

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
  - ⚠️ **Bu bölüm M5-11'den sonra DAHA AZ satır görecek** (eklendi 2026-08-02,
    [ADR 0008](../adr/0008-practice-satiri-ve-yon-zinciri.md)). O tarihe kadar bir
    practice satırı, altındaki **gerçek açık girişi maskeliyordu**: sonraki tap
    `out` yerine `in` yazılıyor ve gerçek giriş hiç kapanmıyordu — yani bu listede
    **iki** satır beliriyordu ve ikisi de "unutulmuş çıkış" gibi görünüyordu.
    `GetLastOpenTransaction` artık `AND NOT t.practice` taşıyor. **Düzeltme ileriye
    dönüktür (§4.3):** o şekilde yazılmış eski satırlar olduğu gibi kalır ve §5'in
    dediği gibi **müdürün manuel kaydıyla** kapanır. ⚠️ Ve bu liste, bir kaydın
    **neden** açık kaldığını (gerçekten unutulmuş çıkış mu, maskelenmiş giriş mi)
    satırın kendisinden **söyleyemez** — ikisi de aynı şekildir. Ayırmak isteyen
    bir rapor, kişinin practice satırının `occurred_at`'ini ayrıca okumak zorunda.
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
- Guardrail'lerin **sırası** da gösteriliyor (M3-05'in **2026-08-02 düzeltme
  bloğundaki** 1→10; [ADR 0007](../adr/0007-guardrail-sirasi-ve-guvenlik-uyarisi.md)
  4–7 bandını değiştirdi — kartın ilk tablosu değil) — müdür bir
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
  - ⚠️ **BU SİNYALİN BUGÜN KAYNAĞI YOK** (eklendi 2026-08-02, M5-10 uygulaması).
    Kriter, M5-10'un `tap_page_views(tag_uid, ctr, first_seen_at, source_ip)`
    tablosunu yazacağını varsayıyordu; o tablo **kullanıcı kararıyla
    yapılmadı.** ⚠️ Yapmama gerekçesi bir **ARGÜMANDIR, ölçüm değil** —
    yapılmamış bir tablonun koruma katkısı ölçülemez, ve bu notun ilk yazımı
    "ölçüldü" diyordu. Argüman: pencere zaten `GET` anından ölçülüyor
    (imzalı `IssuedAt`), saldırgan o `GET`'in **zamanını kendisi seçtiği**
    için "en eski satırı pinlemek" tehdit modelinde bir şey kazandırmıyor;
    tablonun tek gerçek katkısı bu metrikti. Argümanı destekleyen **iki olgu
    ölçüldü**: (a) pencere gerçekten bu imzalı damgadan ölçülüyor (damga
    payload'dan silinince **üç** DB testi kırmızı); (b) oturumsuz `GET /t`
    **303'te duruyor** (`transactions` ve `last_ctr` sabit). Tam gerekçe:
    M5-10 kartının 2026-08-02 düzeltme bloğu, **md. 3**.
    `GET /t` bugün **stateless**'tır: sayfa imzalı bir bağlam mintler ve
    hiçbir yere satır yazmaz, yani "açıldı ama basılmadı" sayısını türetecek
    bir kayıt yoktur. **M6-11 ya kendi kaynağını üretecek** (sayaç/tablo +
    saklama süresi + RLS beşlisi; kimliksiz `GET /t` 303'te durduğu için
    yazma yolu **oturumlu** isteklerle sınırlıdır) **ya da sinyali
    düşürecek.** Kartın kendi cümlesi düşürmeyi kolaylaştırıyor: bu ikincil
    sinyal uçak modu senaryosunda zaten sıfır kalır; A1'in **asıl** izi olan
    `ctr` boşlukları (bir sonraki madde) M5-05'ten beri **canlı**
    (`base:ctr-gap-review` gerçek bir tap'te ateşlendi). Bu not M6-11'i
    çözmüyor, yalnız sahibine doğru bilgiyi bırakıyor.
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
  - ⚠️ **M5-11 bu listeyi kısalttı** (eklendi 2026-08-02,
    [ADR 0008](../adr/0008-practice-satiri-ve-yon-zinciri.md)): practice satırı
    artık altındaki gerçek açık girişi maskelemiyor, yani "çıkışsız açık kayıt"
    sayısı düşüyor. Eski (maskelenmiş) satırlar `transactions` immutable olduğu
    için **duruyor** ve M6-08'in manuel girişiyle kapanır. Ölçüldü (dev/seed DB,
    2026-08-02): 2 502 açık girişin **0**'ı M5-11 öncesinden maskelenmiş
    durumdaydı; o gün ölçülen 13 maskelenmiş satırın **hepsi** M5-11'in kendi
    sondalarının ürünü ve isimlerinden tanınıyor (`M511 Probe`, `Rita Zammit`
    fixture'ı, `Practice Chain`, `Ivan Petrov [sim …]`). ⚠️ `chainFixture` her
    `make test`'te bu şekilden **iki satır daha** bırakır ve §4.3 + `REVOKE DELETE`
    yüzünden temizlenemez — bu listeyi yazan sorgu dev veritabanında onlarla
    karşılaşacak.
  - 🔴 **DEVİR NOTU — maskelenmiş açık girişler bu listeden KENDİLİĞİNDEN düşer,
    yani onları GERİYE DÖNÜK aramak ayrı bir sorgu ister** (eklendi 2026-08-02,
    M5-11 2. tur). "Açık kayıt" testi `NOT EXISTS (… o.type='out' AND
    o.occurred_at > t.occurred_at)`'tir: **tek bir `out`, kendinden eski TÜM açık
    girişleri kapatır**. Dolayısıyla M5-11 öncesinde maskelenmiş bir giriş,
    kişinin **bir sonraki çıkışıyla** birlikte bu listeden sessizce çıkar —
    kapanmış *sayılır*, ama onu kapatan `out` aslında **başka** bir girişin
    çıkışıdır ve arada geçen süre saatlere yanlış girer. Ölçüldü (dev/seed DB,
    2026-08-02): bu şekilden **47** satır hâlâ açık ve listede görünür, **43**
    satır ise sonradan gelen bir `out` yüzünden **artık görünmüyor**.
    (47 sayısı ADR yazıldığında 13'tü; fark `chainFixture`'ın her `make test`'te
    eklediği satırlardır, üretim değil.)
    **Geriye dönük tespit bu listeyle YAPILAMAZ** — ayrı bir sorgu gerekir:
    `EXISTS (… p.practice AND p.occurred_at > t.occurred_at)`, yani "altında
    kendisinden yeni bir practice satırı olan `in`". ⚠️ Bu **§4.6 ihlali DEĞİL**:
    hiçbir satır kaybolmuyor, `transactions` hepsini tutuyor — kaybolan
    **sinyaldir**, kayıt değil.
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
