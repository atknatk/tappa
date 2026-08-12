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
  ⚠️ **KISMEN karşılandı — koşulsuz okuma yanlıştır.** Oran sınırı tamdır; `audit_log` yazımı yalnız **çözümlenen** e-postalar için olur: **bilinmeyen e-postada `failLogin` aday döngüsüne hiç girmez**, dolayısıyla satır yazılmaz (ölçüldü: 0 satır, sahte recorder üzerinde). `audit_log.tenant_id` NOT NULL + `tenants` FK (00005) ise bu boşluğun **neden kapatılmadığının** gerekçesidir — atfedilecek tenant yoktur — **sıfırın mekanizması değil** (nedensellik 8. turda ayrıldı, 10. turda bu satıra da işlendi). Ayrıntı ve sayılar: aşağıdaki **2026-08-03 kart güncellemesi**.
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

> **Kart güncellemesi (2026-08-03) — B fazı teslim edildi.**
> Yeni paket `internal/adminauth` (parola, token, çerez, oturum yaşam döngüsü) ·
> `internal/handler/adminlogin.go` + `logincontext.go` + `adminratelimit.go` ·
> `internal/httpx/adminidentity.go` (`RequireAdmin`) ·
> `web/templates/pages/admin.templ` + `adminview.go` · `cmd/tappa` wiring.
> **Yeni migration YOK** — şema gerçekten hazırdı.
>
> **Beş yükümlülüğün durumu (00011 numaralandırması):**
> 1. **Aynı yanıt — KARŞILANDI, bayt düzeyinde ölçüldü.** Üç sonuç (bilinmeyen
>    e-posta / yanlış parola / devre dışı admin) tek tarayıcıdan sürüldüğünde
>    **401 + 2360 bayt, üçü de birebir aynı**; ayrı tarayıcılardan sürüldüğünde
>    **yalnız `csrf` alanı** normalize edilerek aynı (2323 bayt).
>    `TestAdminLogin_ThreeFailuresAreByteIdentical`.
> 2. **Kukla bcrypt — KARŞILANDI *(bu satır 6. turda düzeltildi; o güne kadar
>    YANLIŞTI)*.** n=20/kol, `-race`siz ölçüm: bilinmeyen e-posta **med 521 ms** ·
>    yanlış parola **med 491 ms** · devre dışı admin **med 491 ms**; **medyan
>    yayılımı 1,06×**. ⚠️ Üç kolun üçü de **≤72 baytlık** parola kullanıyordu;
>    **72 baytı aşan parola şekli ölçülmemişti ve o şekilde yükümlülük TERSİNE
>    dönüyordu.** Güvenlik denetçisi 6. turda buldu; ayrıntı ve düzeltme aşağıdaki
>    **6. tur** bloğunda.
> 3. **Oran sınırı + `audit_log` — KISMEN.** Adres başına 10 başarısız giriş;
>    ölçüldü: 25 POST → 10×401 + 15×429 ve `Authenticate` **tam 10 kez** çağrıldı.
>    Audit bütçesi ölçüldü: **60 adresten 60 başarısız giriş → 11 satır**
>    (10 `failed` + 1 `rate_limited`), 60 değil. **Karşılanmayan yarısı aşağıda
>    LİMİT olarak yazılı.**
> 4. **bcrypt amplifikasyon sınırı — KARŞILANDI, sınır 8.**
> 5. **Aday↔parola bağı — KARŞILANDI, yapısal.** Tek üretici
>    `Authentication.Verified()` (matched **AND** active); `Issue` yalnız
>    `Verified` tipini alır; seçicinin kümesi HMAC'le imzalanır ve ikinci adımda
>    üyelik yeniden kanıtlanır (`selectVerified`). 00011'in canlı ölçtüğü saldırı
>    gerçek satırlarla kuruldu ve reddedildi:
>    `TestAuthenticate_RefusesTheCrossTenantBypass` ·
>    `TestPanelE2E_TenantPickerWithRealRows`.
>
> **⚠️ 00011'İN BİR SAYISI DÜZELTİLDİ, PAHALI YÖNDE.** 00011 amplifikasyonu
> *"cost-10'da bir karşılaştırma ~60–100 ms"* diye ölçekliyor. Bu makinede cost-10
> gerçekten **88–99 ms** — ama **sevk edilen digest'ler cost-12'dir**
> (`test/fixtures/seed.sql`, `$2a$12$`), ve cost-12 **376–427 ms** ölçüldü. Yani
> 500 adaylık şekil **~30–50 sn değil ~190 sn CPU**'dur; 00011'den türeyen her
> sayı **~4× iyimser**. Kukla digest de bu yüzden cost-12'dir.
>
> **🔬 BCRYPT `-race` ALTINDA 19× YAVAŞ — ve bu suite'i bir kez KIRDI.** Ölçüldü:
> bir cost-12 karşılaştırma **-race'siz 0,60 sn**, **-race ile 11,34 sn**. İlk
> uygulamada `internal/adminauth` Go'nun 10 dakikalık paket zaman aşımına takıldı
> (**609 sn, FAIL**). Çare: cost, **konusu olmadığı her yerde** `bcrypt.MinCost`'a
> indirildi (digest kendi cost'unu taşır, doğrulama yolu aynıdır) ve zamanlama
> testinin örneklem sayısı **build tag ile** ayrıldı (`-race`: 3, düz: 20) —
> test **atlanmıyor**, daha az turla koşuyor. Sonuç: `make test` **YEŞİL**,
> 14 paket, 0 FAIL, **301 sn**.
>
> **Verilen kararlar (hepsi ölçümle):**
> - **Aday sınırı 8.** `8 × 10 başarısız deneme × ~380 ms ≈ 30 sn CPU / 10 dk /
>   adres` (≈ bir çekirdeğin %5'i). *"İlk eşleşmede dur"* **REDDEDİLDİ**: aynı
>   parolayı iki işletmede kullanan kişi seçiciyi hiç görmezdi — özelliğin kendisi.
> - **Oran sınırı anahtarı ADRES.** Hesaba göre sınır kurbanı kilitler (e-posta
>   yarı-açıktır); adrese göre sınır dağıtık saldırganı durdurmaz. Kilitlenme
>   müşteriye kesin zarar, CPU sınırı kademeli — adres seçildi, kalanı LİMİT.
>   Hesap bütçesi **yalnız `audit_log` yazımını** sınırlar, hiçbir isteği reddetmez.
> - **72 bayt: REDDET.** Ölçüldü ve bu bir ürün hatasıydı:
>   `GenerateFromPassword` 72 baytı aşanı **reddediyor** ama
>   `CompareHashAndPassword` **sessizce kırpıyor** — 72 baytlık öneki paylaşan iki
>   farklı parola birbirini doğruluyor (`nil` döndü). SHA-256 ön-hash reddedildi
>   (saklama biçimi değişir, sürüm işareti yok, NUL tuzağı).
> - **Adım 1 → adım 2 taşıması: HttpOnly çerezte imzalı bağlam** (`tapcontext.go`
>   kalıbı). Reddedilenler: sunucu tarafı bekleyen-giriş tablosu (migration
>   gerektirir + kimliksiz yolda yazma), parolayı ikinci adımda tekrar sormak
>   (+380 ms ve hiçbir şey bağlamaz). **Tek aday varsa seçici atlanır**, yani blob
>   olağan yolda hiç üretilmez.
>
> **LİMİTLER — kapatılamayanlar, dürüstçe:**
> - **Bilinmeyen e-postalı deneme `audit_log`'a YAZILMAZ.** Ölçüldü: **0 satır**
>   (`TestAdminLogin_UnknownEmailCannotBeAudited`, pozitif kontrollü). Kartın
>   *"başarısız girişler `audit_log`'a yazılıyor"* kriteri **bilinen adresler için**
>   karşılanır, bilinmeyenler için **karşılanmaz**.
>   ⚠️ **Nedensellik düzeltildi (8. tur).** Bu madde sıfırın sebebini
>   `audit_log.tenant_id` NOT NULL + FK'ye bağlıyordu; **yanlış bağlıyordu.**
>   Sıfırın doğrudan sebebi `failLogin`'in **0 adayla döngüye hiç girmemesi**;
>   NOT NULL kısıtı ise *"neden bir sistem-tenant'ı uydurmuyoruz"*un gerekçesidir
>   (yani niçin bu boşluğun kapatılmadığının sebebi, boşluğun mekanizması değil).
>   İkisi de doğru, cümle kaydırılmıştı.
>   ⚠️ Ayrıca *"ölçüldü"* **sahte recorder** üzerinde ölçülmüştür (`fakeTrail`),
>   canlı Postgres'te değil — `TestPanelE2E_SignInAndOut` gerçek `audit_log`'u
>   sayar ama yalnız **bilinen** adres için.
> - **Panel oturumunun SUNUCU tarafı süresi YOK.** `admin_sessions`'ta `expires_at`
>   yok ve ne çözümleyici ne `TouchAdminSession` karşılaştırılabilir bir damga
>   döndürür. Çerezin 12 saatlik `Max-Age`'i **tarayıcıya bir ricadır**; çerezi
>   saklayan bir istemci süresiz kalır. Oturumu bitiren üç şey: açık çıkış,
>   *"her yerden çıkış"*, `admin_users.status='disabled'`. **Devir: M6-05/M7-04**
>   (sütun + sorgu, yani migration).
> - **Dağıtık saldırgan sınırsız.** Adres bütçesi N adreste N× açılır; tek yapısal
>   çare M7-02'nin e-posta doğrulaması (00011 da bunu söylüyor) veya proxy düzeyi
>   sınır (M8).
> - **Aday SAYISI bir zamanlama sinyali.** Üç sonuç ayırt edilemez, ama iki
>   işletmede kayıtlı bir adres ~2× sürer. Kapatılabilirdi (her girişi
>   `maxCandidates` karşılaştırmaya doldurmak) ve **kapatılmadı**: her başarılı
>   giriş dahil **~3,0 sn** ve saldırganın istek başına aldığı CPU **25×** artardı.
> - **Aday sınırı bir KİLİTLENME satın alır.** 8'den fazla işletmede kayıtlı bir
>   adres için sırada geç kalan gerçek satır hiç karşılaştırılmaz. Bugün
>   erişilemez; **M7-02'den itibaren saldırgan 9 tenant açarak kurbanı kilitleyebilir.**
> - **`internal/store/*.sql.go` bu görevde değişti ama kod değişmedi:** A fazı
>   `db/queries/admins.sql`'in **yorumlarını** `make gen`'den sonra düzenlemiş;
>   bu görevin `make gen`'i onları üretilen dosyalara taşıdı (**+98 satır, hepsi
>   yorum, 0 silme**).

> **Kart güncellemesi (2026-08-03, 2. tur — üçüncü göz RED'inden sonra).**
> Denetçi bloklayan **bir** bulgu çıkardı ve doğruydu:
> **`adminauth.CookiePath` sabitinin DEĞERİ hiçbir testle sabitlenmemişti.** Sabiti
> tek başına `"/admin"` → `"/"` yapmak üç paketi de **yeşil** bırakıyordu
> (`adminauth` 23,4 sn · `handler` 116,5 sn · `httpx` 4,3 sn — kendim tekrar
> ürettim), çünkü path'ten söz eden iki test de sabiti **kendisiyle**
> karşılaştırıyordu (`ck.Path != CookiePath`; `HasPrefix(route, CookiePath)` — `"/"`
> ile her rota geçer). **1. turdaki "çerez path'i mutasyonu RED" iddiam yanlıştı:**
> ben sabitin **kullanımını** (`Set()` içinde literal) bozmuştum, **tanımını**
> değil.
> **Çare:** `internal/handler/admincookiepath_test.go` — gerçek `http.CookieJar`
> ile, **üç panel çerezinin üçü için** (`tappa_admin_session`,
> `tappa_admin_login`, `tappa_admin_choice`), **iki durumda** (giriş ortası +
> giriş yapılmış), **iki bağımsız okumayla** (sunucunun gördüğü + jar'ın
> göndereceği) ve **pozitif kontrollü** (çalışan çerezi `/t`'de **görünmeli**).
> Ölçüm: `GET /t while signed into the panel saw exactly: [tappa_session]`.
> Mutasyon: sabit `"/"` → **RED**, hata mesajı sızan çerezleri adıyla sayıyor
> (`[tappa_admin_choice tappa_admin_login tappa_session]`). İki totolojik satır da
> düzeltildi (artık `CookiePath == "/"` durumunu ayrıca reddediyorlar).
>
> **Bloklamayan düzeltmeler (hepsi ölçümle):**
> - **`go.mod` kaydı yanlıştı** — `x/crypto` doğrudan import edilirken
>   `// indirect` yazıyordu. `go mod tidy` onu doğrudan bloğa taşıdı.
>   ⚠️ **Düzeltme (4. tur):** bu satır *"`go.sum`'dan 4 bayat satır sildi"* diyordu;
>   **yanlış**. O iki modül **silinmedi, YÜKSELTİLDİ** — `x/sync 0.21.0→0.22.0`,
>   `x/text 0.39.0→0.40.0` — ve `go.sum`'dan düşen satırlar **eski sürümlerin**
>   satırlarıydı. Meşru bir MVS sonucu (`go mod graph`:
>   `x/crypto@v0.54.0 → x/text@v0.40.0 → x/sync@v0.22.0`), ama bir sürüm
>   yükseltmesini "bayat satır silme" diye yazmak **sayı/olgu hatası** sınıfıdır.
> - **`manager.go` fazla söylüyordu.** *"there is no constructor for it outside
>   `Authentication.Verified()`"* **YANLIŞTI**: `logincontext.go`'nun `parse`'ı
>   imzalı bloptan `adminauth.Verified` kuruyor. Garanti **iki ayaklı** olarak
>   yeniden yazıldı (bu paketin AND'i + HMAC & `selectVerified`). Tek-üreticili
>   yapmak **ölçüldü ve reddedildi**: imzalı çerez BİÇİMİNİ `internal/adminauth`'a
>   taşımak gerekirdi (§3 sınırını ters çevirir) ve sonuç yine ikinci bir üretici
>   olurdu, sadece adı uzun.
> - **Seçim blobu TEK KULLANIMLIK DEĞİL.** Ölçüldü ve testle sabitlendi
>   (`TestAdminChoose_BlobIsNotSingleUse`): tamamlanmış girişten sonra çerezleri
>   geri koyup aynı kümeden başka bir işletme POST etmek **303, oturum 1 → 2**.
>   İki yorum (*"the one path that spends them"*, *"can finish the login"*) buna
>   göre düzeltildi. **Yükselme değil:** küme genişlemiyor — aynı test replay'in
>   küme DIŞINA çıkamadığını da kanıtlıyor. Tek kullanımlık yapmanın bedeli
>   sunucu tarafı durumdur (tablo = migration, ya da yeniden başlatmada kaybolan
>   süreç-içi küme) ve küme genişlemesine karşı **hiçbir şey** satın almaz.
> - **MAC girdisindeki ön-ek belirsizliği YAPISAL olarak kapatıldı** (yoruma
>   bırakılmadı): `sign` artık `label || len(payload) || "|" || payload || "|" ||
>   bind` yazıyor. Eskiden güvenlik `parse`'ın *"tam 3 alan"* kontrolünden
>   geliyordu — **başka bir fonksiyondan** — ve payload'a 4. alan eklendiği gün
>   sessizce açılırdı. Test tek bir dizgenin **her ayırıcısından** bölünerek
>   üretiliyor (elle yazılmış ilk tabloda iki vaka gerçek çakışma değildi; boşluk
>   koruması yakaladı ve tablo üretilen biçime çevrildi), pozitif kontrol eski
>   yapının **çakıştığını** gösteriyor.
> - **Kabul kriteri satırı** artık koşulsuz değil: `audit_log` maddesine
>   **KISMEN** işareti ve bu bloğa atıf konuldu.
>
> **N5 — ÖLÇÜLDÜ, KARAR KULLANICININ (verilmedi).**
> `TestPanelTypes_CarryNoSecretField` sabit **tip** listeli; M5-10'un adlandırdığı
> sınıf. İki okuma:
> **(a) yapısal invaryant** (go/ast ile paketin tiplerini gezip sır-benzeri alan
> arar): ~55 satır, **1,9–2,6 ms**, bugünkü pakette **0 yanlış pozitif**
> (5 dışa açık string alanının 0'ı işaretlendi), ve negatif kontrolde yeni eklenen
> `AdminProfile.PasswordHash`'i **YAKALADI**. Sınırı: ad tabanlı, nötr adlı bir
> alan (`Value string`) kaçar.
> **(b) LİMİT olarak sayıp yazmak:** sabit liste **7 tip** sayıyor, paket ise
> **10 dışa açık struct** taşıyor (`Token`, `Manager`, `Cookies` listede yok —
> bugün zararsız, hiçbirinde dışa açık string alan yok). Aynı negatif kontrolde
> sevk edilen test **YEŞİL** kaldı: yeni bir tipe karşı gerçekten çaresiz.

> **Kart güncellemesi (2026-08-03, 4. tur — yeni denetçi, İKİ bloklayan).**
>
> **B2 — totoloji sınıfının KARDEŞİ.** `adminauth.MaxCandidates`'i tek başına
> `8`→`9` yapmak iki paketi de yeşil bırakıyordu (kendim tekrar ürettim), çünkü
> `TestAuthenticate_CapsTheCandidateLoop`'un her beklentisi sabitin **kendisiyle**
> yazılmıştı. 00011 bu sayıyı *"the COST limit, MEASURED, and the largest number in
> this file"* diye adlandırıyor — yani **sayının kendisi teslimattır**.
> **İki ayrı kusur, ikisi de kapatıldı:**
> 1. **Çapraz-paket eşitlik artık YAPISAL.** `MaxCandidates` dışa açıldı ve
>    `adminChoiceMaxEntries = adminauth.MaxCandidates` oldu. Eskiden iki bağımsız
>    literal + bir yorum vardı ve sonucu ölçüldü: cap 9 iken **9 işletmeli meşru
>    bir yönetici HTTP 500 alıyordu** (`8 → 303`, `9 → 500`). Artık cap'i tek
>    başına değiştirmek handler'ı **yeşil** bırakıyor (derive edildiği için) ve
>    `TestAdminLogin_FullSizeVerifiedSetReachesThePicker` cap'teki kümenin
>    picker'a **303** ile ulaştığını sürüyor.
> 2. **DEĞER pinlendi.** `TestMaxCandidates_IsTheMeasuredCPUBound` literal `8`'i ve
>    aritmetiği (`8 × 10 × 380 ms ≈ 30 sn CPU/10dk/adres`) taşıyor; cap testinin
>    tablosu artık literal. Mutasyon `8→9` → **iki test de kırmızı**.
>
> **🔴 AİLE TARAMASI — bu bulgunun asıl dersi, sayıyla.** M6-01 B fazının getirdiği
> **16 sabit** tek tek, **yalnız değeri** değiştirilerek tarandı:
> **9'u pinliydi, 7'si PİNSİZDİ** (`cookieMaxAgeSeconds` 12sa · `adminFloodLimit`
> 200 · `adminAccountLimit` 10 · `adminAttemptPeriod` · `adminLoginCookieMaxAge` ·
> `adminChoiceCookieMaxAge` · `adminChoiceFutureSkew`). **Yedisi de pinlendi**
> (`TestPanelConstants_ShippedValuesArePinned` +
> `TestAdminAuthConstants_ShippedValuesArePinned`, her satır kendi gerekçesini
> taşıyor) ve yeniden tarandı → **16/16 PİNLİ**.
> Tarama ayrıca **B2'nin bir kardeşini daha** buldu: `adminChoiceCookieMaxAge`
> `adminChoiceTTL`'i *"deliberately equal"* diyen bir yorumla ikinci kez yazıyordu
> → artık `int(adminChoiceTTL / time.Second)` olarak **türetiliyor**.
> ⚠️ Tarama sırasında bir **10 dakikalık timeout ağaçta canlı mutasyon bıraktı**
> (`adminAttemptPeriod = 60 * time.Minute`); doğrulama adımı yakaladı ve geri
> alındı. Agent-brief'in M5-06 dersi bu turda tekrar ateşlendi.
>
> **B1 — `make check` TEMİZ AĞAÇTA KIRMIZIYDI.** 3. turdaki *"yalnız `git diff`
> kapısında düştü"* iddiam **çürütüldü**: `make test` adımında düşmüştü.
> `TestAuthenticate_TimingIsFlat` 5 tam koşuda 1 kez kırmızı (1,59× > 1,50× kapısı).
> **Kullanıcı kararı (2026-08-03) uygulandı:**
> - **Yükümlülük 2'nin dayanağı artık YAPISAL testler** ve yorum bunu söylüyor:
>   *kukla KOŞUYOR* (`TestAuthenticate_DummyIsReallyRun`) + *DOĞRU COST'ta*
>   (`TestCost_MatchesTheDummyDigest`, `TestSeedDigests_UseTheDeclaredCost`).
>   İkisi de **tam** kontrol — integer karşılaştırması, istatistik değil.
> - **Kapı ölçülmüş gürültüye göre kondu.** Gerçek koşulda (tam suite, `-race`,
>   14 paket eşzamanlı) **10 koşu**: medyan yayılım **min 1,00 · medyan 1,01 ·
>   maks 1,07**; tek kolun kendi max/min'i **medyan 1,07 · maks 1,56**. Denetçinin
>   daha yüklü makinesinde: **1,59** (kırmızı veren koşu), kol içi **2,3×**.
>   ⚠️ İki veri kümesi **aynı değil** — benim makinem boştu; bu M5-08'in
>   *"eşzamanlılık ölçümü sessizce artefakt üretir"* dersidir, o yüzden kapı
>   **iki kümenin en kötüsüne** göre seçildi.
> - **Sinyal de ölçüldü** (varsayılmadı): kukla **tamamen yok** → **515594×** ·
>   kukla **cost 10** → **4,04×** · kukla **cost 11** → **1,91×**.
> - **Kapı: `-race` altında 2,5× · `-race`siz 1,5×.** Gerekçe her ikisinde de kendi
>   gürültüsü: race kolunda en kötü gözlenen 1,59'un **1,57× üstünde**, en yakın
>   sinyal 4,04'ün **1,62× altında**; race'siz kolda gözlenen gürültü 1,00 olduğu
>   için 1,5× kapısı **1,91× vakasını da yakalıyor**.
> - **LİMİT:** `-race` kapısı artık **2,5×'ten küçük** bir zamanlama kaçağını
>   göremez — somut olarak **bir cost adımı** sapmasını (1,91×) **kaçırır**. Bu
>   yalnızca cost uyuşmazlığı **tam** olarak pinlendiği için kabul edilebilir.
>   Gerçekten korumasız kalan: 2,5×'in altında, ne eksik kukla ne cost uyuşmazlığı
>   olan **üçüncü bir şekil**.
>
> **Süre — kullanıcı kararı uygulandı.** `make test-short` artık bcrypt **duvar
> saati örneklemini** de atlıyor: **156 sn → 39,80 sn** (`/usr/bin/time -p`).
> Suitte **tam 2 SKIP** ve **ikisi de Makefile yorumunda adıyla sayılı**.
> `make test` **değişmedi** ve commit kapısı aynı. Yükümlülük 2 `-short`'ta da
> koşuyor.
> ⚠️ **Bu paragrafın iki sayısı sonradan geçersizleşti — 12. turda işaretlendi.**
> (a) **SKIP sayısı artık 2 değil 3**: 6. tur HTTP zamanlama testini ekledi
> (`TestPanelE2E_TimingIsFlatOverHTTP`), üçü de Makefile yorumunda adıyla sayılı;
> doğrulayan komut `go test -race -count=1 -short -v ./... | grep -c -- "--- SKIP:"`
> → **3**. (b) **Süreler**: `make test` ~300 sn **değil**, o rakam paket
> sürelerinin toplamıydı; duvar saati **111–116 sn**, `make test-short`
> **gözlenen aralık 51–74 sn** (18. turda üç yük durumunda: boş 50,9–51,9 · 4
> çekirdek 54,9–57,0 · 8 çekirdek 73,6; bağımsız denetçi 69,9–74,2 — 8 çekirdek
> koluyla birebir örtüşüyor. Bu sayı görevde üç kez dar yazıldı ve üç kez tutmadı;
> artık `make test` gibi **gözlem kaydı** olarak yazılıyor) (16. turda iki
> koşulda ölçüldü ve Makefile'a koşuluyla yazıldı; 10. turun 37,2–41,4 ve
> 12. turun 50–57 bantları o turlardan sonra eklenen testlerle geçersizleşti). Bu blok tarihli
> olduğu için metni değiştirmiyorum; dosyanın geri kalanındaki gibi ⚠️ notuyla
> işaretliyorum — 10. turun F-2'si `day_db_test.go`'yu düzeltmiş ama bu satırı
> atlamıştı ve dosyada iki farklı standart kalmıştı.
>
> **Bloklamayan:**
> - **N1 — kabul edilen güvenlik açığı artık yazılı:** `GO-2026-5932`,
>   *"golang.org/x/crypto/openpgp is unmaintained, unsafe by design"*,
>   `Found in: golang.org/x/crypto@v0.54.0`, **`Fixed in: N/A`**. **Yükselterek
>   kapatılamaz.** Bu repo `x/crypto`'dan **yalnız `bcrypt`**'i import ediyor
>   (ölçüldü: **4** import satırı — `password.go`, `password_test.go`,
>   `manager_db_test.go`, `manager_timing_test.go` — dördü de `bcrypt`;
>   1. turda "2", 4. turda "3" yazmıştı, dördüncüsü 8. turda eklendi ve sayı
>   10. turda düzeltildi), `openpgp` çağrılmıyor →
>   govulncheck *"0 vulnerabilities affect your code"* diyor ve `make audit`
>   **exit 0**. Q03 `x/crypto`'yu onaylıyor. **Kabul edilen limit olarak burada
>   sayılıyor** (M1-07→M1-09 dersi).
> - **N3 — `tapSurfacePaths` artık KAYNAKTAN türetiliyor.** Elle yazılmış liste
>   gerçek 6 rotanın **4'ünü** sayıyordu (`/activate/tour` ve `/api/activate`
>   eksikti) ama yorumu *"the real paths"* diyordu. Şimdi paket kaynağı taranıyor
>   → **6 rota** (`TestEmployeeRoutes_DerivationIsNotVacuous` boşluk koruması
>   olarak bilinen 6'yı zorunlu tutuyor).
> - **N5 — yapısal invaryant SEVK EDİLDİ** (kullanıcı kararı):
>   `TestPackageTypes_NoExportedCredentialField`, paketin tiplerini go/ast ile
>   gezip sır-benzeri **dışa açık ham alan** arıyor. Ölçüm: **10 struct, 5 dışa
>   açık ham alan, 0 işaretli**, ~2 ms. Negatif kontrol
>   (`TestPackageTypes_InvariantCatchesANewType`) sabit listeli testin **kaçırdığı**
>   `AdminProfile.PasswordHash`'i yakalıyor. **LİMİT: ad tabanlıdır** — nötr adlı
>   bir alan (`Value string`) kaçar; taşınması gereken değerler için asıl koruma
>   hâlâ redakte eden tip (`db.PasswordHash`, `adminauth.Token`).
> - **N4 — panel flood kapıları KAPATILDI** (ölçüldü: **tek tablo testi, 4 alt
>   test, 0,01 sn, yeni altyapı YOK** → devretmeye değmez).
>   `TestAdminAuth_FloodCeilingRefusesEveryUnauthenticatedRoute`; mutasyon (bir
>   rotadan `flooded` çağrısını kaldır) → **kırmızı**.
>   ⚠️ **Bu ölçüm bir bulgu da çıkardı** ve **12. turda KAPATILDI** —
>   ayrıca aşağıdaki 12. tur bloğuna bak. O tur bu paragrafın **cümlesini de
>   çürüttü**: *"kimliksiz değiller … çalınmış bir çerezi olan biri"* **YANLIŞTI**.
>   `adminauth.Token.hash` yalnız bir **şekil** kapısı uygular (43 karakter,
>   base64url), yani **uydurma** bir token da resolver'a ulaşıyordu; tehdit modeli
>   *"çalınmış çerez"* değil **herkes**ti. `GET /admin` ve `POST /admin/logout`
>   artık `a.floodGate` arkasında. **Çalışan tarafındaki 3 rota** (`/t`, `/activate*`, `/api/*`) hâlâ
>   testsiz ve **altyapı istiyor** (Tap/Activation koşum takımı: DB, invite
>   yöneticisi, oturum yöneticisi, SUN doğrulayıcı) — M5-07'den devredilen borç,
>   M6-01'in kapsamı değil.

> **Kart güncellemesi (2026-08-03, 6. tur — `tappa-security-auditor` RED, GERÇEK DELİK).**
> Genel üçüncü göz aynı turda ONAY vermişti; güvenlik denetçisi §4 merceğiyle
> baktı ve **yükümlülük 2'yi yıkan bir zamanlama kehanetini** buldu. Bu projede
> ikisinin birlikte koşmasının sebebi tam olarak budur (M2-04, M5-10 ile aynı desen).
>
> **🔴🔴 KRİTİK — kendi Q03 düzeltmem deliği açmıştı.** `Compare` 72 baytı aşan
> parolada bcrypt'e **hiç girmeden** `false` dönüyordu; `Authenticate` kuklayı
> **yalnız aday yokken** ödüyordu. Sonuç yükümlülüğün **tam tersi**: kayıtlı
> e-posta HIZLI, kayıtsız e-posta YAVAŞ. **Kendim tekrar ürettim** (domain, n=5):
> `20 bayt: bilinen 203,9 ms / bilinmeyen 197,0 ms → 0,97×` ·
> `100 bayt: bilinen 66 ns / bilinmeyen 211,4 ms → **3.203.211×**`.
> Denetçinin HTTP tablosu: `100 bayt bilinen **5,53 ms** vs bilinmeyen 295,4 ms
> → **53,43×**`. Tek istekle, istatistiksiz, internet gecikmesinin çok üstünde:
> *"bu adres bir panel yöneticisi mi?"* sorusuna kesin cevap. Üstelik sunucuya
> **sıfır bcrypt**'e mal oluyordu, yani adres bütçesinin koruduğu CPU bile
> harcanmıyordu. 00011 bunu B fazına **zorunlu** devretmişti ve birebir bu
> cümleyle: *"skipping it is a timing oracle wide enough to measure over the
> internet."*
>
> **Düzeltme:** `Compare` uzun parolada **aynı digest'e karşı** kırpılmış bir
> karşılaştırma koşuyor ve sonucu **atıp** `false` dönüyor. `CompareDummy` yerine
> *bu* digest seçildi çünkü satırın **kendi cost'unu** öder (eski bir cost-10 satır
> ya da cost-4 fixture'da da yassı kalır). Sonuç kullanılmadığı için sessiz kırpma
> da yeniden doğmuyor. ⚠️ `Authenticate`'in başına erken-dönüş **koyulmadı**:
> süreyi yassılatırdı ama `Attempts`'i boşaltıp §4.6 audit izini silerdi.
>
> **Doğrulama (dördü de):**
> 1. Zamanlama testine **iki yeni kol** (uzun parola × bilinen/bilinmeyen), toplam
>    **5 kol**: `195,7 / 207,9 / 200,7 / 195,7 / 196,4 ms`, yayılım **1,06×**.
> 2. **HTTP tablosu yeniden üretildi** (gerçek router + gerçek Postgres, cost-12
>    satır, min n=2): `20B bilinen 202,6 ms · 20B bilinmeyen 198,2 ms ·
>    100B bilinen **205,1 ms** · 100B bilinmeyen 197,4 ms` → **1,04×**
>    (53,43× idi). ⚠️ İlk deneme n=3 ile **kendi oran sınırıma** takıldı
>    (12 başarısız giriş > 10) — sınır çalışıyor; test bütçeyi yükseltmek yerine
>    n=2'ye indi.
> 3. **Mutasyon** (erken dönüşü geri koy): domain **235.621×** → kırmızı,
>    HTTP **47,75×** → kırmızı.
> 4. **Yükümlülük 4 yeniden ölçüldü.** Uzun parola artık aday başına tam bcrypt
>    ödüyor (**önce bedavaydı**): 8 aday, in-range **1,608 sn** (201 ms/aday) vs
>    uzun **1,708 sn** (214 ms/aday). Yani uzun yol **en kötü duruma yükseldi,
>    onu aşmadı** — `8 × 10 × ~380 ms ≈ 30 sn` sınırı ve
>    `TestMaxCandidates_IsTheMeasuredCPUBound`'un gerekçesi **hâlâ geçerli**.
>    Değişen: ucuz prob artık ücretli, yani bütçe gerçekten probu da sınırlıyor.
>
> **Yanlışlanan üç cümle gerçeğe indirildi:** `password.go`'nun *"caller still pays
> a dummy"* ve *"decided entirely by the attacker's own input"* iddiaları (ikisi de
> **yanlıştı** — `CompareDummy`'yi handler hiç çağırmıyor, `grep` → 0; ve hızlı
> dala yalnız **sunucu durumu** aday üretince giriliyordu) ve bu kartın *"2. Kukla
> bcrypt — KARŞILANDI"* satırı.
>
> **Güvenlik denetçisinin diğerleri:**
> - **S1 (§4.6) — hesap audit bütçesi bir İZ SUSTURMA primitifi.** 11 istekle bir
>   hesabın bütçesi yanar ve pencerenin kalanında o hesaba yapılan **her**
>   başarısız giriş yazılmaz — *"devre dışı hesapta DOĞRU parola"* dahil.
>   *"Kilitleme yok"* doğruydu, **eksikti**: kilitleme yok, **susturma var**.
>   Pencere başına bir `rate_limited` satırı kaldığı için **kesinti, körlük değil**;
>   satır artık `SuppressedFrom` (ilk bastırılan denemenin sırası) taşıyor.
>   **LİMİT:** toplam sayı DB'den kurtarılamaz — gerçek adet, pencere **kapanınca**
>   yazılan bir satır ister ve `httpx.Limiter`'ın süre-sonu kancası yok (tembel
>   tahliye eder), yani altyapı. Sayıldı ve devredildi.
> - **S2 (§4.7)** — *"Compare … is the ONLY caller of RevealForPasswordComparison"*
>   → `Compare` **çağrılandır**; tek üretim çağrı yeri `Authenticate`. Garanti
>   doğruydu, cümle yanlıştı; düzeltildi.
> - **S3** — *"etiket, payload'ın alfabesinde geçemeyen bir baytla biter"* →
>   etiket `|` ile bitiyor ve payload'ın ayırıcısı da `|`. Zararsızdı (belirsizliği
>   kapatan şey **uzunluk öneki** ve dört ayrı türetilmiş anahtar), **cümle**
>   düzeltildi — etiket değiştirilmedi (hiçbir şey satın almaz, uçuştaki blobları
>   geçersiz kılar).
> - **S4 — patlama yarıçapı yazıldı.** `TAPPA_SESSION_HMAC_KEY` sızarsa sonuç
>   yalnız *"çalışan oturumu forge etme"* değil: denetçi gerçek türetilmiş
>   anahtarla imzaladığı sahte payload'la seçiciyi kurbanı gösterir hâle getirdi
>   (200) ve POST **303 + kurbanın tenant'ında 1 oturum** üretti — yani
>   **parolasız çapraz-tenant panel oturumu**. DÜŞÜK kalmasının sebebi ön koşul:
>   kurbanın `admin_user_id` **ve** `tenant_id`'si (**244 bit**, hiçbir sayfada,
>   URL'de veya hata metninde görünmez). Yükümlülük 5 bağı etkilenmiyor —
>   sunucunun imzalamadığı her şeyi reddediyor; bu saldırı **sunucu olarak
>   imzalayabilmeyi** varsayıyor.
>
> **Genel üçüncü gözün sayı/cümle bulguları — hepsi kapatıldı:**
> - **`make test` süresi yanlıştı.** Kart/Makefile `~300 sn` diyordu; o rakam
>   **paket sürelerinin TOPLAMI** (aynı ağaçta **254,5 sn**). Duvar saati
>   (`/usr/bin/time -p`, 3 koşu): **112,1 / 113,1 / 115,9 sn** (denetçi 92-112 sn).
> - **`make test-short`:** **37,7 / 38,2 / 40,7 sn** (denetçi 47,1-51,2 sn).
>   Her iki aralık da Makefile'a yazıldı.
> - **`x/crypto` import satırı 2 değil 3.**
> - **`-short` skip sayısı 2 değil 3** oldu (HTTP zamanlama testi eklendi); üçü de
>   Makefile yorumunda **adıyla** sayılı.
> - **"ALL 16 CONSTANTS"** → 16 **sayısal alt kümedir**; faz ~29 sabit getiriyor
>   (etiketler, çerez adları, `adminCSP`, `dummyDigest`, `redacted`,
>   `adminChoiceVersion`). Cümle indirildi. Ve sayısal kümede eksik olan
>   **`hmacKeyLen`** pinlendi (mutasyon `32→16` → **kırmızı**); daha önce yalnız
>   **yapısal** olarak korunuyordu, **değerle** değil.
> - **"en yakın sinyal 4,04"** → en yakın sinyal **1,91×** ve **kapının ALTINDA**
>   (bilerek feda edildi). Aynı dosya bunu LİMİT'te doğru söylüyordu; çelişki
>   kaldırıldı.
> - **Seçim blob'unun en kötü ömrü 5 değil ~6 dakika** (`adminChoiceTTL` +
>   `adminChoiceFutureSkew`; ölçülen kabul bandı `[-1m .. 5m]` = **6m1s**). Yazıldı.
> - **`adminChoiceMaxEntries`'in iki gerekçesi birbirine bağlandı**: 4 KB çerez
>   garantisi artık **CPU** sınırından türetilen bir sayıya biniyor (64 girişte
>   ~4,7 KB). Bugün hareket edemez (değer pinli) ama yazıldı.

> **Kart güncellemesi (2026-08-03, 8. tur — yeni güvenlik denetçisi: 6. TURUN
> DÜZELTMESİ KENDİ SAVUNMASINI KALDIRMIŞ.)** Denetçi kritik düzeltmeyi doğruladı ve
> genişletti (13 parola şekli × bilinen/bilinmeyen, hepsi ≤1,04×; HTTP 6 hücre ×
> n=10 → 1,05×; aday sayısı doğrusal, cap 9'da ve 500'de de 8,0×'te tutuyor;
> eşleşme konumu sızmıyor; Go bcrypt'in NUL tuzağı yok) — sonra **üç bulgu** çıkardı.
>
> **🔴 F1 — üçüncü `-short` skip'i, kapattığım 53× kehanetin İÇ DÖNGÜDEKİ TEK
> savunmasını sildi.** 6. tur kehaneti kapattı ama **yalnız duvar saati
> testleriyle** pinledi ve **aynı turda ikisini de `-short`'tan çıkardı**.
> Denetçi `Compare`'i geri çevirdi (kehanet yeniden açık) ve
> `go test -count=1 -short ./...` **14/14 YEŞİL** verdi. Ürün korumasız değildi
> (commit kapısı tuttu) ama günde onlarca kez koşan mod kördü — ve Makefile
> **tersini** beyan ediyordu. *"Bir cümle sistemin vermediği bir şeyi beyan
> ediyor"* sınıfının **beşinci** vakası, ve bu kez **düzeltmenin kendisi** üretti.
> **Çare:** `TestAuthenticate_OverLongPasswordStillPaysBcrypt` — **cost-4** fixture
> digest'i, taban **200 µs**; dürüst karşılaştırma **1,47 ms**, bozuk dal **66 ns**
> → **dört büyüklük mertebesi** pay, duvar saati örneklemi gerekmiyor, her modda
> koşar. Makefile cümlesi **üç** yapısal testi adıyla sayacak şekilde düzeltildi.
> **ASIL KANIT:** aynı mutasyonla
> `go test -count=1 -short ./...` → `FAIL internal/adminauth` (önce 14/14 ok idi).
>
> **🔴 F2 — `SuppressedFrom` hiçbir testle tutulmuyordu.** Mutasyon `n → 0` ile
> **tüm handler paketi yeşil** kalıyordu; tanımlayıcı 3 kaynak satırında ve kartta
> geçiyor, **0 testte**. S1'in *"körlük değil kesinti"* savunmasının tamamı bu
> alana biniyor. **Çare:** `TestAdminLogin_SuppressionRowCarriesItsStartingOrdinal`
> — 15 adresten 15 başarısızlık → **10 yazılı + 1 rate_limited, SuppressedFrom=11**
> (denetçinin sayısıyla birebir); mutasyon → **kırmızı**.
>
> **🔴 AİLE TARAMASI YENİDEN KOŞULDU (4. turdan sonra eklenen her öğe).**
> **6 öğe tarandı: 1'i pinliydi, 5'i PİNSİZDİ.** Pinsizlerin **dördü test
> EŞİĞİYDİ** (`timingSpreadGate` ×2 build, `httpTimingGate`, iki duvar saati
> tabanı) — M5-10 sınıfının en baştan çıkarıcı biçimi: *"kırmızıya dönen bir
> zamanlama testinin doğal onarımı kapıyı genişletmektir, ve genişletilmiş kapı
> silahsızlandırılmış kapıdır."* **Beşi de pinlendi**, ama **DEĞER olarak değil
> ÖZELLİK olarak**: her kapı, kendi ölçülmüş **gürültüsünün üstünde** ve yakalaması
> gereken **en zayıf sinyalin altında** olmak zorunda. Yeniden tarandı → **6/6**.
> ⚠️ **Bu invaryant kendi yazarını da yakaladı:** ilk hâli `-race`siz kapıyı (1,5×)
> `-race` gürültüsüne (1,59×) karşı ölçüyordu ve **kırmızı verdi** — haklı olarak,
> çünkü 1,59× yarış dedektörü + eşzamanlı paketlerin artefaktı ve o derlemede
> **yok**. Gürültü çıpası da build-tag'lendi (race 1,59 · düz 1,10). M5-08'in
> *"eşzamanlılık ölçümü artefakt üretir"* dersinin küçük ölçekli tekrarı.
>
> **🔴 F3 — `Compare`'de DÖRDÜNCÜ zamanlama kolu, DIGEST tarafında.** Denetçi
> ölçtü: `password_hash = ''` → **198 ns**, bozuk digest → **154 ns**, kukla
> (cost-12) → **297,9 ms** — yani **~1,5-1,9 milyon kat**. Sebep bcrypt'in kendisi:
> boş/kısa/geçersiz digest'te **49-215 ns** içinde hata döner, anahtar programını
> hiç kurmaz. Kapattığım kehanetin şekli **digest tarafından ve ters yönde**
> yeniden açılır. **Bugün erişilemez** (doğrulandı: `db/queries` ve üretim Go'sunda
> `INSERT INTO admin_users` yok, `password_hash` UPDATE'i yok — tek
> `UPDATE admin_users` `MarkAdminLoggedIn`, yalnız `last_login_at`; seed iki satır
> da `$2a$12$`; `Hash()` boşu ve >72'yi reddediyor). **Ama şema
> `password_hash text NOT NULL`, format CHECK'i YOK** → `''` şema-geçerli, ve yazma
> yolunu açacak görevler adıyla belli: **M6-05 · M7-04 · M7-02**. 00011'in
> amplifikasyon sınırıyla **aynı yapı**: *"bugün erişilemez ve bu sebep süresi
> dolacak."* **Devredilen kural tek cümle:** `admin_users.password_hash` yalnız
> `adminauth.Hash` çıktısıyla yazılır. Sütun CHECK'i **yapısal** çare olurdu —
> migration olduğu için **yapılmadı**, seçenek olarak yazıldı.
>
> **F4 (düşük, ikisi de cümle):**
> - **Bilinen e-postalı başarısızlık N adet `audit_log` gidiş-dönüşü öder**,
>   bilinmeyen ödemez. ⚠️ **Bu satır %0,3 diyordu — DÜŞÜKTÜ (12. turda düzeltildi).**
>   Kapanış denetimi üç kolu ayırdı (n=15): satır **yazılırken** +%1,67, yazım
>   **bastırılmışken** −%0,27; tekrarlar +%1,25/+%1,63 ve +%1,38/+%1,88. Kendi
>   ölçümüm: gerçek Postgres'te **bir `audit_log` satırı min 1,89 / med 2,51 /
>   maks 3,66 ms**, yani ~300 ms'lik bir girişin **aday başına ~%0,84'ü**. Dürüst
>   bant **~%0,8–1,9**. **Vargı değişmedi:** farkın tamamı yazımdır (bastırılınca
>   sıfıra iner, ölçüldü) ve büyüklük loopback'te ölçümün kendi gürültüsüyle aynı
>   mertebede — bir koşuda medyan **−%1,21** çıktı. *"Aday SAYISI bir sinyaldir"*
>   limitinin yanına yazıldı.
> - **Kartın nedenselliği kaydırılmıştı** (bilinmeyen e-postada 0 satırın sebebi
>   `tenant_id` NOT NULL **değil**, `failLogin`'in 0 adayla döngüye girmemesi) ve
>   *"ölçüldü"* **sahte recorder** üzerindeydi. İkisi de düzeltildi.

> **Kart güncellemesi (2026-08-03, 10. tur — sayı hijyeni turu).** Yeni bir genel
> üçüncü göz **18 mutasyon** koştu ve *"ürün kodunda bloklayan kusur bulamadım"*
> dedi; yeşil kalan 7 mutasyonun hiçbiri erişilebilir açık değil, hepsi test-ağı
> kalitesi. Bir bloklayan vardı ve yine **bir düzeltmenin içinden** doğdu.
>
> **🔴 F-1 (bloklayan) — 8. turda Makefile'ı düzelttim, KAYNAĞINI bırakmışım.**
> Makefile *"üç yapısal test"* diyordu ama işaret ettiği kanonik dosya beş yerde
> hâlâ *"two"* diyordu — ve en kötüsü **canlı `-short` skip mesajı** yükümlülük
> 2'nin kanıtı olarak yalnız **iki** test adı veriyordu, 8. turun tam da bu boşluk
> için eklediği `TestAuthenticate_OverLongPasswordStillPaysBcrypt`'i saymıyordu.
> Yani `-short` koşan geliştirici **kapsamı eksik beyan eden** bir mesaj okuyordu.
> **Sayı hassasiyeti düzeltildi:** doğru ifade *"üç test"* değil,
> **DÖRT test fonksiyonu / ÜÇ başarısızlık şekli** (cost kanıtı iki fonksiyon).
> Canlı kanıt (`go test -short -v -run TestAuthenticate_TimingIsFlat ./internal/adminauth/`):
> mesaj artık dördünü de adıyla sayıyor.
>
> **🔴 F-6 — eşik invaryantının KENDİ ÇIPASI pinsizdi (listenin en önemlisi).**
> Denetçi ölçtü: `worstObservedNoise 1,59 → 1,00` **tek başına** yeşil; `gate → 1,5`
> **ve** çıpa birlikte indirilince de yeşil — yani kırmızı bir kapının **doğal
> onarımı** invaryantı geçiyor ve *"5 koşuda 1 kırmızı"* diye ölçülmüş 1,50×
> kapısını geri getiriyordu. **Çare, yeni mekanizma değil repodaki mevcut şekil**
> (`guardrails_test.go`): üç build-tag'li sabit (`timingSamples`,
> `timingSpreadGate`, `worstObservedNoise`) artık **KAPALI BİR KÜME** —
> `legalTimingConfigs`, iki adlandırılmış tuple, saf `isNamedTimingConfig`
> yüklemi ve **iki kalıcı negatif kontrol**. Herhangi **bir** değeri değiştirmek
> eşleşen satır bırakmıyor; üçünü birden değiştirmek de birinin **yazdığı** bir
> satıra düşmek zorunda. **Kanıt:** kapı+çıpa **birlikte** indirilince →
> `{samples:20 gate:1.20 noise:1.00} matches no named tuple` → **KIRMIZI**.
> ⚠️ **Bu mekanizma kendi tasarımcısını da yakaladı:** negatif kontrole *"kapı+çıpa
> birlikte"* vakasını **oran yüklemine** koymuştum ve kontrol **kırmızı verdi** —
> haklı olarak, çünkü çıpa 1,00 iken 1,5 kapısı gerçekten gürültü ile sinyal
> arasındadır. Oran yüklemi **düzenlenmiş çıpayı göremez** (çıpa onun girdisidir);
> onu yalnız kapalı küme görür. Vaka doğru teste taşındı ve bu ayrım yorumda yazılı.
>
> **F-7 ve F-8 aynı mekanizmayla kapandı:** kapının sessizce %60 genişletilmesi
> (`2,5 → 4,0`) ve `timingSamples 3 → 1` artık tuple'ı bozuyor → **kırmızı**.
>
> **Diğer pinler (hepsi mutasyon + pozitif kontrol):**
> - **F-9** `ErrPasswordTooLong` **hiçbir testte iddia edilmiyordu** — `Hash`'in
>   guard'ı kaldırılınca paket yeşil kalıyordu, çünkü bcrypt'in **kendi** hatası
>   `err != nil`'i karşılıyordu. Q03'ün *"reddet"* kararını bu repoda tutan tek
>   satırdı. Pinlendi (sentinel + limitin adı + parolanın hata metninde geçmemesi);
>   mutasyon → **kırmızı**.
> - **F-10** `parse`'ın `len(parts) != 3` katılığı ağsızdı (`< 3`'e gevşetince
>   yeşil). Gerçek anahtarla imzalanmış ama **alan sayısı yanlış** payload'larla
>   pinlendi (4/2/1/5 alan) + pozitif kontrol; mutasyon → **kırmızı**.
> - **F-11** uzun-parola testinin girdisi 72 bayta indirilince **geçiyordu** —
>   *"bcrypt koştu"* ve *"eşleşmedi"* sıradan yanlış parola için de doğru. Test
>   artık **girdi uzunluğunu kendisi iddia ediyor**; mutasyon → **kırmızı**.
>
> **Sayı/cümle düzeltmeleri — her sayı kendi komutuyla yeniden ölçüldü:**
> - **F-2** `day_db_test.go` *"THE ONLY testing.Short() SKIP IN THIS REPOSITORY"*
>   diyordu → **üç** skip var (`grep -c -- "--- SKIP:"` → **3**), ve Makefile bu
>   dosyayı *skip #1* diye sayıyor: iki dosya birbirini gösterip farklı sayı
>   söylüyordu. Aynı blok `make test 84,7-98,5 sn` / `test-short 32,9 sn`
>   taşıyordu → yeniden ölçüldü (`/usr/bin/time -p`, üçer koşu):
>   **111,4 / 112,1 / 113,9 sn** ve **37,2 / 39,5 / 41,4 sn**.
>   ⚠️ **`test-short` bandı 12. turda geçersizleşti:** o turun testleri (`floodGate`
>   tablosu, NUL/UTF-8, alan sayısı, sabit pinleri) ~10 sn geri ekledi; yeniden
>   ölçüldü → **50,1 / 51,1 / 51,3 / 56,4 sn**, Makefile güncellendi.
>   `make test` değişmedi (112,0 / 112,2 / 112,5 sn).
> - **F-3** skip mesajı *"3 arms"* diyordu, tablo **5 kol**; hata mesajı *"the
>   three outcomes"* diyordu. İkisi de düzeltildi.
> - **F-4** `x/crypto` import satırı: kart *3* diyordu, ölçüm **4**
>   (dördüncüsünü 8. turda ben ekledim). Dayandığı sonuç değişmiyor.
> - **F-5** geri çekilen nedensellik iki yerde daha duruyordu (kabul kriteri satırı
>   ve `adminlogin.go`); ikisi de düzeltildi: **sıfırın mekanizması `failLogin`'in
>   döngüye hiç girmemesi**, NOT NULL kısıtı ise boşluğun **kapatılmama** gerekçesi.
> - **F-12** `password.go`'da bozuk tırnak (`so ” is schema-valid`) düzeltildi.
>
> **⚠️ TUR SIRASINDA ORTAYA ÇIKAN, M6-01'E AİT OLMAYAN BİR KUSUR — sayıldı, kapsam
> dışı olduğu için DÜZELTİLMEDİ.** Doğrulama partisinin bir koşumunda `make test`
> kırmızı bitti: `TestConsumeInvite_ConcurrentRaceExactlyOneWinner`
> (`internal/db`) → `FATAL: sorry, too many clients already (SQLSTATE 53300)`.
> **Mantık hatası değil, bağlantı yuvası tükenmesi.** Aritmetik:
> `max_connections=100`, `superuser_reserved_connections=3` → `tappa_app`'e **97**;
> iki **mevcut** kırmızı-çizgi yarış testi **her biri 54** bağlantılık havuz açıyor
> (`internal/db/invites_test.go` `raceInviteGoroutines=50` ve
> `internal/sun/advance_test.go` `raceGoroutines=50`, ikisi de +4) →
> **54+54 = 108 > 97, tek başlarına**. Yani iki paket örtüştüğünde suite **zaten**
> sınırın üstünde; M6-01 sebep değil. Tek başına koşturulduğunda test **3/3 yeşil**.
> **M6-01'in payı ve yapılan:** bu görev eşzamanlı koşan bir DB havuzu daha ekliyor
> (pgx varsayılanı `max(4, NumCPU)`), oysa bu paketin testleri **sıralı**. Kendi
> ayak izimi `pool_max_conns=4`'e indirdim — kendi kapsamım, dürüst azaltma.
> Sonrasında **`make check` 5/5 yeşil** (ağaç shasum'ı değişmedi).
> **Çare M6-01'in değil:** kök neden `internal/db` + `internal/sun` test
> altyapısında ve goroutine sayılarını düşürmek **§4.4 kırmızı-çizgi testlerini
> zayıflatır**. Devredilen seçenekler: `max_connections`'ı yükseltmek (docker
> compose), iki yarış testini aynı pakete toplamak, ya da paket düzeyinde
> serileştirme. **Orkestratöre devir.**

> **Kart güncellemesi (2026-08-03, 12. tur — KAPANIŞ).** Kapanış güvenlik denetimi
> **beşinci zamanlama şeklini aradı ve bulamadı** (12 hücrelik tabloda yayılım
> **1,04×**, negatif kontrolü **57,72×**), beş kanalı ayrıca sondaladı, yükümlülük
> 5'i takas dâhil kendi saldırı matrisiyle yeniden kırmayı denedi ve **geçemedi**;
> 13 mutasyonun 13'ü kırmızı. Üç iş kaldı, üçü de kapatıldı.
>
> **🔴 F-A (bloklayan) — `GET /admin` KİMLİKSİZ bir çağırana bütçesiz resolver
> okuması ödetiyordu, ve LİMİT cümlem bunu inkâr ediyordu.** Kendim tekrar ürettim
> (200 istek/şekil): çerezsiz **med 5,9 µs**, **uydurma** 43 karakterlik token
> **med 1,56 ms** — yani gerçek bir resolver okuması — ve **600 istekte HİÇ 429
> yok**. `adminauth.Token.hash` yalnız **şekil** kapısı uyguladığı için saldırganın
> çözümlenecek bir oturumu **olması gerekmiyordu**; *"çalınmış bir çerezi olan
> biri"* cümlesi yanlış öncüldü ve M6-02'ye devir notu da ona dayanıyordu.
> Üstelik `manager.go:518` **doğru** söylüyordu (*"an unauthenticated flood costs
> reads, not writes"*) — repo iki şey söylüyordu.
> **Çare (kullanıcı kararı): korumalı gruba `a.floodGate`**, `Protect`'ten
> **ÖNCE** — reponun kendi sırası (`tap.go`: `ByAddress` → `Identify` →
> `BySession`; `httpx/router.go` bunu açıkça savunuyor). Yeni mekanizma yok, aynı
> bütçe, aynı sayaç. ⚠️ **Sayı ayrımı (bu turda kendi taslağımda karıştırmıştım):**
> `a.flooded` **çağrı yeri** 4 → **5**'tir (dört handler + bir middleware, çünkü
> iki korumalı rota tek `floodGate` üzerinden geçer); **kapsanan ROTA** sayısı
> 4 → **6**'dır. İkisi aynı sayı değil.
> **Doğrulama:** (i) 600 uydurma-token isteği → `map[303:200 429:400]`, **ilk 429
> tam #201'de** (tavan 200); (ii) flood tablo testi artık **altı** rota (⚠️ **18. turda beşe indi**: çıkış satırı 16. turda çıkarıldı — ölçülüyor ama reddedilmiyor — ve kendi değişmez testine taşındı); (iii)
> mutasyon (`floodGate` satırını sil) → **iki alt test kırmızı**; (iv) pozitif
> kontrol: **100 ardışık kimliği doğrulanmış sayfa yüklemesinin 100'ü de 200**.
>
> **🟡 F-C — NUL / geçersiz UTF-8 → HTTP 500. Yazmak yerine KAPATILDI.** Ölçüm:
> `NUL 500 / 1,43 ms` · `geçersiz UTF-8 500 / 1,43 ms` · `sıradan bilinmeyen
> 401 / 201,6 ms`. Kehanet değildi (dal tamamen saldırgan girdisiyle belirlenir)
> ama üç şey birden yanlıştı: kimliksiz bir yolda **500**, tek biçimli reddin
> **dışında**, ve **bedava** (failLogin hiç koşmadığı için deneme bütçesi de
> yüklenmiyordu). Çare `adminauth.Authenticate`'in başındaki mevcut boş-alan
> dalına eklendi (`isLookupableEmail`: NUL + `utf8.ValidString`, yani **yalnız
> Postgres'in reddettiği baytlar** — "geçerli e-posta" tanımı **değil**, o ikinci
> bir doğruluk kaynağı olurdu). Kukla yine ödeniyor. **Sonuç ölçüldü:** üçü de
> **401**, **~198–202 ms**, ve gövdeler **bayt-bayt aynı (2360 bayt)**. Yükümlülük
> 1'in *"aynı yanıt"* testine bir kol daha eklendi — kazanç, maliyet değil.
>
> **🟡 F-B — iki sayı yanlıştı, ikisi de benim yazdığım.** (1) *"%0,3"* →
> gerçek bant **~%0,8–1,9**; kendi ölçümüm: bir `audit_log` satırı gerçek
> Postgres'te **min 1,89 / med 2,51 / maks 3,66 ms** = ~300 ms'lik girişin **aday
> başına ~%0,84'ü**. **Vargı korundu** (farkın tamamı yazımdır ve loopback
> gürültüsüyle aynı mertebede — bir koşuda medyan −%1,21). (2) 4. tur bloğundaki
> *"tam 2 SKIP … ikisi de"* → bugün **3**; blok tarihli olduğu için metin
> korunup dosyanın geri kalanıyla **aynı standartta ⚠️ notu** aldı (10. turun
> F-2'si `day_db_test.go`'yu düzeltmiş, bu satırı atlamıştı — dosyada iki farklı
> standart kalmıştı).

> **Kart güncellemesi (2026-08-03, 14. tur).** Kapanış denetçisi 12 turun tamamını
> yeniden doğruladı (B1–B10 yeşil, yükümlülük 5 canlı iki-tenant saldırısıyla
> kırılamadı, `utf8.ValidString` ≡ Postgres UTF8 denkliği 30 bayt şekliyle
> **0/30 sapma**, yükümlülük-1 tablosu 10 hücrede **hepsi 401 · 2360 bayt · tek
> sha256 · 1,05×**). İki bloklayan çıktı; **ikisi de bir düzeltmenin ürünüydü.**
>
> **🔴 F1 — `isLookupableEmail` guard'ının üretim yolunda SIFIR ağı vardı.** Guard
> silinince **tüm suite yeşil** (kendim tekrar ürettim). Sebep: onu adıyla anan tek
> test `newFakeManager` ile koşuyordu ve **sahte DB her bayt dizisini kabul eder**,
> yani test pinlediğini iddia ettiği dalı **hiç görmüyordu**. Aynı sınıfın
> **üçüncü** vakası ve yine kendi düzeltmemden doğdu (12. tur F-C).
> **`newFakeManager` taraması — sayılar iki hâlde:** tarama sırasında **6 üst düzey
> test / 8 çağrı yeri**, beşi meşru (bcrypt maliyeti, döngü sınırı, kukla — DB'nin
> söz hakkı yok), **1'i kördü**. Kör test silindikten sonra **5 test / 7 çağrı
> yeri** kaldı; hiçbiri DB'nin reddettiği bir şeyi kanıtlıyormuş gibi yapmıyor.
> Çare: `TestAuthenticate_RefusesAnUnlookupableAddress_RealDB` (gerçek Postgres) +
> iki pozitif kontrol. **Kanıt:** guard silinince **tam suitte VE `-short`'ta
> kırmızı**; yalnız NUL satırı devre dışı bırakılınca **iki NUL vakası kırmızı,
> UTF-8 vakası yeşil**. **Kör test SİLİNDİ** — kanıtlamadığı hâlde bir ağ olarak
> sayılıyordu ve `-short`'a **10,7 sn** mal oluyordu.
>
> **🔴 F2 — 12. turdaki kararın ürettiği regresyon: flood kapısı ÇIKIŞI
> reddedebiliyordu.** Tekrar ürettim: kurbanın **kendi** 100 sayfa yüklemesi
> pencerenin yarısını harcadı, adres anahtarını paylaşan biri kalanını yaktı,
> kurbanın `POST /admin/logout` → **429, `Revoke` 0 kez**. O pencerede oturumu
> bitirecek **başka hiçbir yol yok**. **Değişmez kısıt uygulandı: çıkış üçüncü bir
> tarafça reddedilemez.** `POST /admin/logout` artık **ölçülüyor ama
> reddedilmiyor** (`meterOnly`), önünde resolver'dan **önce** koşan **bedava** bir
> same-origin kapısı var. **Ve desen tamamlandı:** `tap.go`'nun **üçüncü aşaması**
> eklendi — `sessionGate`, **oturum UUID'siyle** anahtarlanmış (token/hash değil,
> §4.7), `adminSessionLimit = 300`. ⚠️ **16. tur bu cümleyi düzeltti:** desen "tamamlanmadı" — `sessionGate` resolver'dan **sonra** koştuğu için maliyeti sınırlamaz, ve 300 türetilmedi kopyalandı; ayrıntı 16. tur bloğunda. **Aritmetik yeniden türetildi:**
> `adminFloodLimit` **200 → 3000** (eski 200 yalnız girişlerden türetilmişti;
> 12. tur kalkanı kimliği doğrulanmış rotaların önüne koyunca aynı 200 her sayfa
> yüklemesini taşımaya başladı). 3000 = `httpx.tapAddressLimit`, bilinçli.
> ⚠️ **bcrypt sınırını genişletmez** — onu `adminAttemptLimit` (10) tutar.
> **M6-02 bu sayıyı yeniden saymalı.**
> **Kanıtlar:** bütçe yanıkken çıkış → **303, `Revoke` 1 kez** · F-A hâlâ kapalı
> (**3400 istek → 3000×303 + 400×429, ilk 429 #3001**) · mutasyon → **kırmızı**.
>
> **🟡 F6 — `Protect()` dışa açıktı, bütçe değildi.** Garanti bir **yorumda**
> yaşıyordu; M6-02 kendi grubunda mount etseydi bütçesiz resolver okuması geri
> gelirdi. `Protect()` artık üç aşamayı besteliyor; kendi grubunda mount edilen bir
> dashboard rotası **#3001'de 429** alıyor.
>
> **🟡 F3 — audit asimetri bandı yine düşüktü, ve "gürültünün içinde" iddiası
> GERİ ÇEKİLDİ.** İki koşulda: **boş 2,29 ms / 205,6 ms = %1,11**; **4 çekirdek
> meşgul 3,64 ms / 222,3 ms = %1,64**. Denetçinin uçtan uca yazım-farkı **+%2,58**.
> Dürüst bant **~%1–3** (~2–10 ms). Yapısal iddia ayakta (bastırılınca +%0,58) ama
> *"ölçümün gürültüsüyle aynı mertebede"* **yanlıştı** — 15 iç içe turda iki kol
> temiz ayrışıyor. Artık **gerçek, ölçülebilir, kapatılmamış** bir asimetri.
>
> **🟡 F4 — `day_db_test.go` geri çekilen bandı taşıyordu.** 10. turun F-2'si
> *"iki dosya farklı sayı söylüyor"* diye açılmıştı ve **kendi düzeltmesi aynı
> kusuru ters yönde üretti**. Çare sayıyı düzeltmek değil **tek yere indirmek**:
> süreler artık yalnız Makefile'da.
>
> **🟡 F5 — `make test` bandı tekrar üretilemedi.** Artık **iki koşulda** ölçülüyor
> ve koşul yazılıyor: `make test` **boş 113,3/113,8/114,9**, **meşgul 120,5/121,1**;
> `make test-short` **boş 51,2/51,3/53,1**, **meşgul 54,3/58,3**. Makefile bandın
> bir **hedef değil gözlem kaydı** olduğunu ve bağımsız ölçümlerin **92–150 sn**'ye
> yayıldığını söylüyor. ⚠️ `test-short` bu turda önce **66–67 sn**'ye çıkmıştı;
> flood tablosunun bütçeyi **rota başına değil bir kez** yakması ve kör testin
> silinmesi geri getirdi.

> **Kart güncellemesi (2026-08-03, 16. tur — SON MEKANİZMA TURU).** 15. tur
> denetçisi ürünün sağlam olduğunu ölçtü (yükümlülük 1 **10 hücrede tek sha256**,
> yükümlülük 5 **yedi canlı saldırıda** ayakta, B1–B10 mutasyonla kırmızı,
> `newFakeManager` taraması temiz, flake 7 koşuda 0) — ama **14. tur hiç
> denetlenmemişti** ve iki bloklayan çıkardı. **İkisi de yine kendi düzeltmemin ağı
> ve sayısı hakkındaydı.**
>
> **🔴 F-1 — `sessionGate` hem SIFIR AĞA sahipti hem koruduğunu söylediğini
> korumuyordu.** İki mutasyon (asla reddetme · `Protect`'ten tamamen sil) **tam
> ağaçta yeşil**; pinli olan yalnız sabitin **değeriydi**. `isLookupableEmail` ile
> **birebir aynı desen, dördüncü kez**. Ve aşama **maliyetin yanlış tarafında**:
> `Verify` = resolver okuması **+ transaction + `TouchAdminSession` UPDATE**, gate
> ondan **sonra** koşuyor → **reddedilen istek maliyeti zaten ödemiş** (429 alan
> istek `last_used_at`'i değiştirdi; 3000 istek → **3000 Verify**, 2700×429).
> *"Çalınmış çerezin sınırı 300"* **yanlıştı** — DB sınırı **3000**.
> **Mekanizma taşınmadı** (oturumu çözmeden oturuma göre anahtarlanamaz;
> `tap.go`'da sorun yok çünkü `httpx.Identify` bilerek **yazmaz**). **İki şey
> yapıldı:** (1) davranışsal ağ — **300 servis, ilk ret #301**, aynı adresten
> **başka bir oturum etkilenmiyor** (pozitif kontrol); iki mutasyon da **kırmızı**.
> (2) cümle ölçüme indirildi: **`floodGate` (3000/adres) okuma+UPDATE'i sınırlar;
> `sessionGate` (300/oturum) kimlik doğrulamadan SONRAKİ işi sınırlar — bugün o iş
> boş, M6-02 dolduruyor.**
>
> **🔴 F-2 — 15× gevşetmenin gerekçesi YANLIŞ KOLDA ölçülmüştü.** Yazılı olan
> *"3000 × ~1,5 ms = ~4,5 sn = %0,75"*; 1,5 ms **uydurma-token** kolu, oysa tavan
> **kimliği doğrulanmış sayfa yüklemeleri** için genişletilmişti. Kendi ölçümüm
> (gerçek Postgres, n=200/kol, **iki yük durumunda**):
> `çerezsiz 0,083 / 0,172 ms` · `uydurma token 0,650 / 0,875 ms` ·
> **`CANLI oturum 3,040 / 4,161 ms`** (denetçi: 0,142 / 1,206 / **5,717 ms**).
> Doğru aritmetik: **3000 × 3,0–5,7 ms = 9–17 sn/pencere/adres = bir çekirdeğin
> %1,5–2,9'u** — yazılanın **2–3,8 katı**. **3000 yine de haklı** (meşru gereksinim
> ~2000; %2,9 ödenebilir) — **değişen sayı değil cümle**, ve *"reddedilen istek de
> ödüyor"* artık aritmetiğin **içinde**.
>
> **🟡 F-4 — çıkışın bedeli "bütçesiz" değil SINIRSIZDI; üçüncü yol seçildi.**
> Denetçi: **10000 anonim `POST /admin/logout` → 10000 resolver okuması, hiçbiri
> reddedilmedi** (kontrol: 10000 `GET /admin` → 3000 okuma + 7000×429). Ürünün tek
> tavansız rotası, tap yüzeyinin **aynı havuzuna** sınırsız anonim erişim
> veriyordu. Seçilen: çıkışa **kendi** tavanı,
> `adminLogoutLimit = 10 × adminFloodLimit = 30000`.
> ⚠️ **14. turun değişmezinden DAHA ZAYIF ve öyle yazıldı:** önce *"üçüncü taraf
> çıkışı asla reddedemez"*di; şimdi *"**30000 istek** harcamadan reddedemez"* —
> panelin geri kalanını engellemenin **on katı**, flood log'unda gürültülü.
> Karşılığında sonsuz yükselteç kapandı. **Ölçüldü:** 30050 çıkış → **50 ret,
> resolver 30000 kez** · panel tavanı yanık + 3000 çıkış harcanmışken **kurban yine
> çıkış yaptı (303, `Revoke` +1)**. Mutasyon → **kırmızı**.
>
> **🟡 F-5 — `sameOriginGate`'in ağı yoktu.** Silinince suite yeşildi çünkü
> `Logout` Origin'i **tekrar** kontrol ediyor → **sonuç** aynı, değişen **maliyet**.
> Ağ artık **resolver çağrısını sayıyor**: yabancı Origin → **0 okuma**, Origin yok
> → **0 okuma**, meşru çıkış → **1 okuma**. Mutasyon → **kırmızı**.
>
> **🟡 F-3 — `adminSessionLimit = 300` TÜRETİLMEDİ, KOPYALANDI → 11. limit.**
> Dosya adres tavanını *"10 admin × 20 görüntüleme × 10 parça ≈ 2000"* diye
> türetiyor → **yönetici başına ~200 istek/pencere**; oturum tavanı 300, payı
> yalnız **1,5×**. `tap.go`'nun 300'ü **başka bir şekle** ait. **Somut sonuç:**
> yoğun bir gün geçiren **meşru** yönetici 301. istekte kendi paneline 429 yer —
> kilitlenme değil (çıkış çalışıyor, ölçüldü) ama panel o pencerede ölü. **M6-02
> bu sayıya adres tavanından DAHA ACİL bir türetme borçlu.**
>
> **🟡 F-6 — bayat sayı** (*"200 requests per 10 minutes"* → **3000**) düzeltildi.

> **Kart güncellemesi (2026-08-03, 18. tur — SON TUR).** Kapanış denetçisi **ONAY**
> verdi (*"zero findings meet the narrow blocking definition"*); 16. turun beş ağı
> da gerçek, tavanlar ürünün kendi koşumundan yeniden üretildi
> (**#3001 · #301 · 30050→50 ret, resolver tam 30000**), `make gen` 252 dosyada
> bayt-idempotent, flake 8 koşuda 0. Kalan yedi madde ucuz olduğu için commit'ten
> **önce** kapatıldı.
>
> **1 — BEŞİNCİ KÖR AĞ: `meterOnly`'nin ücretlendirmesi.** Denetçi `Charge`'ı
> sabitle değiştirdi → **tüm paket yeşil**; `meterOnly` repoda üç yerde geçiyordu
> (iki yorum, bir `r.Use`) ve **sıfır testte**. `isLookupableEmail` ·
> `sessionGate` · `sameOriginGate` ile aynı desen, **beşinci kez**. Ağ, çıkışın
> **paylaşılan** kovayı gerçekten yaktığını ölçüyor: **2500 çıkıştan sonra
> `/admin/login` tam 500 istek daha servis edildi** (tavan 3000). Mutasyon →
> **kırmızı**.
>
> **2 — 🔴 BİR YORUM, VAR OLMAYAN BİR AUDIT İZİNİ BEYAN EDİYORDU.** *"the audit
> trail already carries the successful comparison"* — denetçi ölçtü: çok-adaylı dal
> `a.record`/`a.log` **çağırmıyor**, `internal/adminauth` **0** satır yazıyor, altı
> `a.record` yeri **başka yollarda**. Yani **≥2 gerçek hesapta doğrulanmış bir
> parola**, o hesaplar iki adım arasında devre dışı bırakılırsa **hiçbir yerde iz
> bırakmıyordu** — ve cümle sonraki okuyucuya **bakmamasını** söylüyordu. **M5-11
> sınıfının ta kendisi.** **Cümle yumuşatılmadı, SATIR YAZILDI**: `verified[0]`'ın
> tenant'ına (yani bu isteğin **doğruladığı** bir tenant'a, çağıranın adlandırdığı
> birine değil) `admin.login.failed` + *"password verified but every business was
> disabled before the choice"*. Ölçüm: **1 audit satırı**, doğru tenant,
> `VerifiedBusiness=2`. Mutasyon (satırı sil) → **kırmızı**.
>
> **3 — 12. LİMİT: 30000 tavanının maliyeti hiç hesaplanmamıştı.** Flood tavanı
> aritmetiğini gösteriyor, ürünün **en geniş** tavanı hiçbir şey göstermiyordu.
> Dosyanın kendi uydurma-token ölçümünden: **30000 × 0,65–1,21 ms = 19,5–36,3
> sn/pencere/adres = bir çekirdeğin %3,3–6,0'ı** — flood tavanının **iki katı**, ve
> **tap yüzeyinin paylaştığı havuzda**. **Düşürülmedi**: her istek, *"üçüncü taraf
> çıkışı reddedemesin"* eşiğini yaklaştırır; tek VPS'te %6 ödenebilir, oturumunu
> bitiremeyen müşteri değil. **Eksik olan karar değil SAYIydı** ve artık yazılı.
>
> **4 — kendi içinde çelişen yorum.** *"ALL SIX PANEL ROUTES"* diyordu; tabloda
> **beş** satır vardı, `RequireAdmin` arkasında **bir** tane, ve **aynı blok üç
> satır sonra** çıkışın bilerek dışarıda olduğunu söylüyordu. 16. tur çıkış
> satırını çıkarıp başlığı okumamıştı. Hem test yorumu hem kartın bayat *"altı
> rota"*sı düzeltildi.
>
> **5 — 2. turda geri çekilen ifade, seçiciyi RENDER EDEN dosyada duruyordu.**
> `adminview.go` hâlâ Businesses'ın `Verified()`'den kurulduğunu söylüyordu; seçici
> yolunda **`adminChoices.parse`**'tan geliyorlar, ve `manager.go` bu ifadeyi
> **açıkça yasaklıyor** (*"an audit refuted exactly that wording"*). 2. tur
> `manager.go`'yu düzeltip render eden dosyayı süpürmemişti. Garanti artık **iki
> ayağıyla** yazılı (parola karşılaştırması + imzalı kümenin üyelik yeniden kanıtı).
>
> **6 — ÖLÜ TANIMLAYICI ATIFLARI: 15 tane, tam sayı.** Denetçi üçünü doğrulamış ve
> *"test dosyalarında on bir tane daha"* demişti; **tüm depo gövdesine** (yorum
> olmayan her satır, `.tools` hariç) karşı taranınca **15** çıktı — **2 üretim +
> 13 test**. Onüçü mevcut adına yönlendirildi (`maxCandidates`→`MaxCandidates`,
> `TestCookieSeparation`→`TestPanelCookies_NeverReachTheTapSurface`,
> `problemAdminSignIn`→`problemAdminRestart`, …); ikisi **silinmiş** şeylere
> yapılan tarihsel atıflardı (M5-09 yardımcısı ve 14. turda silinen kör
> test) ve **ada değil OLAYA** atıf yapacak şekilde yeniden yazıldı.
> ⚠️ **İlk düzeltme turundan sonra İKİ tane kaldı** — biri yeniden yazarken
> **benim ürettiğim**, biri gözden kaçan bir tanesi — yeniden tarandı ve ikisi de
> olaya atıf yapacak şekilde düzeltildi. **Son tarama: 0.**
>
> **7 — `make test-short` bandı üretilmiyordu** (denetçi 69,9 / 70,6 / 74,2 ölçtü,
> yazılı bant 55–62). **Üç yük durumunda ölçtüm:** boş **50,9/51,4/51,9** · 4
> çekirdek **54,9/57,0/57,0** · 8 çekirdek **73,6** — sekiz çekirdek kolu
> denetçinin aralığıyla **birebir örtüşüyor**, yani fark makine durumuydu. Bant
> artık tek bir dar aralık değil **GÖZLENEN ARALIK 51–74 sn**, ve `make test`'in
> taşıdığı *"hedef değil gözlem kaydı"* uyarısını artık bu da taşıyor. ⚠️ Bu sayı
> görevde **üç kez** dar yazılıp **üç kez** tutmamıştı (37–41 → 50–57 → 55–62);
> dördüncüsünü yazmamak için biçim değiştirildi.











---

## M6-02 — Dashboard iskeleti ve docket bileşenleri

- **Bağımlılık:** M6-01
- **Araç:** skill `tappa-brand`
- **Commit:** `feat(ui): add dashboard layout and docket components`

**Amaç.** Tekrar eden desenleri bileşen olarak bir kez yazmak.

**Dokunulacak dosyalar.** `web/templates/layout/`, `web/templates/components/`,
`web/static/css/input.css`, `web/templates/pages/`, `internal/handler/`
*(Düzeltildi 2026-08-04: kart `layouts/` diyordu, repoda dizin **tekil** `layout/`.
Ayrıca iş yalnız CSS+bileşen değil — kabuk `pages/` + `handler/`'a da dokunuyor.)*

**Kabul kriterleri.**
- Bileşenler: `docket`, `stamp` (approved/flagged/rejected/**ignored**/training),
  buton, boş durum, sekme navigasyonu, ~~filtre çubuğu~~ → **M6-03'e TAŞINDI**
  (kullanıcı kararı 2026-08-04; gerekçe aşağıdaki düzeltme bloğunda, madde
  **M6-03 kartında** da yazılı — iki kart aynı şeyi söylüyor).
  *(Düzeltildi 2026-08-04: kart **dört** damga sayıyordu, damga **beş**tir ve beşi de
  M0 scaffold'undan beri var. `ignored` bir verdict'tir (§5 satır 5), `result.templ`
  onu **markup'ta** basıyor ve `TestCompiledCSS_StampWordIsInk` **beşini birden**
  arıyor — yani kartın listesi eksikti, kod değil.)*
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
  *(Güncellendi 2026-08-04, kullanıcı kararı — M5-06'nın damga ölçümüyle **aynı
  disiplinde**. `.docket-label` bu kriteri **M0'dan beri ihlal ediyordu** ve hiçbir
  test görmüyordu: `text-ink/50` @10px, en kötü zemin `porcelain` üstünde
  **3,13:1**, AA'nın istediği **4,5:1**'in altında. Ton **`text-ink/70`** yapıldı →
  en kötü **5,70:1**. `/60` ölçüldü ve **REDDEDİLDİ** (paper 4,36:1 · porcelain
  4,18:1 — düzeltmeyen bir düzeltme). **12 çağrı yerinin hepsi** aynı anda değişti,
  çalışanın tap ve onay ekranları dahil; §9'un *"tap ekranı kutsaldır"* kuralı
  **özellik eklemeye** dairdir, okunmayan bir etiket sadeleştirme değildir.
  ⚠️ **Damga anatomisine dokunulmadı** — `opacity-80` geri gelmedi.
  Ağ: `TestBrand_DocketLabelClearsAAOnEveryGroundItSitsOn` — **ürünün kontrast
  oranı HESAPLAYAN ilk testi**, ve `app.css` değil **`input.css` + `tailwind.config.js`**
  okuduğu için `TestCompiledCSS_*` ailesinin aksine **CI'da gerçekten koşuyor**.)*
  *(**Genişletildi 2026-08-04, kullanıcı kararı — denetim `.docket-label`'ın YALNIZ
  BAŞINA olmadığını ölçtü: altı ton daha AA'nın altında sevk edilmişti**, ve biri
  (`punchless`) **ürünün her ekranında** render ediliyordu — yani bu görev beş panel
  ekranı ekleyerek onun **render sayısını artırmıştı**. Altısı da `/70`'e çıkarıldı;
  ton, boyut, zemin ve hesaplanan oran:*
  | Yer | Eski ton / boyut | Zemin (denetlendi) | Eski | Yeni |
  |---|---|---|---|---|
  | `layout/base.templ` `punchless` | `ink/40` @10px | porcelain | **2,40:1** | **5,70:1** |
  | `pages/admin.templ` panel notu | `ink/60` @12px | porcelain | **4,18:1** | **5,70:1** |
  | `pages/activate.templ` config notu | `ink/50` @11px | paper (`Card`) | **3,23:1** | **6,05:1** |
  | `pages/activate.templ` Wi-Fi notu | `ink/60` @14px | paper (`Card`) | **4,36:1** | **6,05:1** |
  | `pages/activate.templ` Problem ipucu | `ink/60` | paper (`Notice ToneAlert`) | **4,36:1** | **6,05:1** |
  | `pages/activate.templ` Confirm ipucu | `ink/60` | paper (`Notice ToneAlert`) | **4,36:1** | **6,05:1** |
  *⚠️ **Bağlayıcı zemin `porcelain` DEĞİL, `green-lite`** (L=0,8229 < 0,8627) — iki
  tur boyunca porcelain sanıldı, türetilmiş test düzeltti; `/60` **dört zeminin
  dördünde de** kalıyor (4,11–4,36), `/70` **dördünde de** geçiyor (5,58–6,05).*
  *⚠️ **WCAG 1.4.3 logotype istisnası `punchless` için değerlendirildi ve KULLANILMADI**
  — gerekçe `base.templ`'de yazılı (marka adı "tappa", bu ayrı bir span, veri
  tipografisinde, ve her ekranda render ediliyor).*
  *Ağ genişletildi: `TestBrand_EveryInkToneClearsAA` **her** `text-ink/NN` tonunu
  tarar. Bilinen delik yazılı: zemin eşleştirme yerine **en koyu açık zemin**
  kullanılıyor; fark yalnız `/61–/63`'te sonucu değiştiriyor ve **standart Tailwind
  adımlarının hiçbiri** o aralıkta değil (mutasyonla doğrulandı: eşdeğer mutant).)*
- `make gen` sonrası `*_templ.go` commit edildi.

> **Kart düzeltmesi (2026-08-04, M6-02 uygulaması sırasında).**
>
> **Görevin ilk adımı kartı ölçmekti ve kartın yarısı zaten sevk edilmişti — ama
> devir notundaki tarih de yanlıştı.** Ölçüm (`git log -S`, `git show 7e12f37`):
> `.docket`, iki perforasyon sözde-elemanı, `.docket-label` **ve beş damga
> varyantının beşi de** **M0 scaffold'unda** (`7e12f37`) doğdu — M5-06'da değil.
> M5-06 (`bb7635b`) damganın **anatomisini** değiştirdi (kelime → `ink`, renk →
> çerçeve, `opacity-80` kaldırıldı), varlığını değil. Perforasyon için **hiçbir
> zaman görsel dosya olmadı** (`git log --diff-filter=A -- web/static/img/*` → boş),
> yani *"saf CSS `radial-gradient`, görsel dosya yok"* kriteri **M0'dan beri**
> karşılanıyordu.
>
> **Gerçekten eksik olan dördü:** buton · boş durum · sekme navigasyonu · filtre
> çubuğu. Üçü bu görevde sevk edildi; **filtre çubuğu sevk EDİLMEDİ** ve gerekçesi
> aşağıda — kullanıcı kararı bekliyor.
>
> **🔴 FİLTRE ÇUBUĞU M6-03'E TAŞINDI — kullanıcı kararı 2026-08-04. Gerekçe:
> bu kartın kendi kapsamı onu dürüstçe sevk etmeyi imkânsız kılıyor.**
> Kart *"içeriklerini YAZMA — onlar M6-03…M6-09"* diyor, yani M6-02'de **filtrelenecek
> hiçbir veri yok**. Bir filtre çubuğu ancak üç hâlde sevk edilebilirdi ve üçü de
> kötü: (a) kullanılmayan CSS kuralı → *"her kural gerçek bir `class=`'a iz sürmeli"*
> kuralını çiğner; (b) ekranda **atıl kontroller** → panelin yapamadığı bir şeyi
> yapabiliyormuş gibi gösterir; (c) *"şu tarihte kayıt yok"* diyen sahte bir sonuç →
> sistemin **ölçmediği bir şeyi beyan etmek**, bu deponun en pahalı hata sınıfı.
> **İki okuma, kullanıcıya:**
> **A — şimdi sevk et (atıl):** kartın maddesi kapanır; bedeli bir ölü kural + çağıranı
> olmayan bir bileşen + yalan söyleyen kontroller.
> **B — M6-03'e ertele (KULLANICI SEÇTİ, 2026-08-04):** filtre çubuğu, altı
> filtresinin (tarih, lokasyon, departman, çalışan, verdict, channel) **var olduğu**
> yerde yazılır ve orada çalıştığı **kanıtlanabilir** — o altı filtre M6-03 kartının
> **kendi kriter satırında** zaten sayılı. Bedeli: bu kartın bir maddesi M6-02'de
> kapanmıyor, ve bu yüzden **düşürülmedi, taşındı**.
>
> **🔴 HTMX SEVK EDİLMEDİ — ve bu da bir çatal.** Ölçüm: repoda HTMX'in **kodu yok**
> (11 eşleşmenin **hepsi düzyazı**: CLAUDE.md §1 tablosu, ADR 0001, marka skill'i,
> iki bütçe yorumu). Ağ erişilebilir (`unpkg` → 200), yani vendor'lamak **mümkündü**.
> Yapılmadı çünkü **iskeletin takas edilecek hiçbir parçası yok**: sekmeler düz
> `<a href>`, yani swap edilecek fragment sıfır. Sevk etseydim `adminCSP`'nin
> `default-src 'none'`'unu bir **script** için genişletmem gerekirdi — ve
> `adminlogin.go` tam olarak bunun tersini yazıyor: *"none of these screens loads a
> script, so naming one would widen the policy for nothing."* **CSP bu görevde
> DEĞİŞMEDİ** (gerçek yanıttan doğrulandı, aşağıda). **İki okuma:**
> **A — şimdi vendor'la:** M6-03 hazır bulur; bedeli bugün gerekçesiz bir CSP
> genişletmesi + kullanılmayan ~48 KB.
> **B — M6-03'te vendor'la (seçilen):** ilk gerçek fragment ile birlikte gelir,
> CSP o commit'te **görünür** bir kararla genişler.
> `TestPanelScreens_LoadNoScriptAndReachNoThirdParty` ilk `<script>`'te **kırmızıya
> döner**, yani sessiz miras imkânsız.
>
> **📏 DEVRALINAN ÜÇ SAYI — ÖLÇÜLDÜ (gerçek sunucu + gerçek Postgres + gerçek oturum).**
> Yöntem: taze oturum, ardışık 305 × `GET /admin`.
> **Sonuç: 300 × `200`, 301. istek `429`** — yani **bir sekme görüntülemesi = tam 1
> ücretlendirilen istek**. Oturum bütçesi tükendikten sonra `GET /static/css/app.css`
> hâlâ **200** döndü → stylesheet `Protect()`'in **dışında** ve panel bütçesine
> **yazılmıyor**.
> | Sayı | Kartın/dosyanın varsayımı | M6-02 iskeletinde ÖLÇÜLEN |
> |---|---|---|
> | `adminSessionLimit = 300` | ~10 parça/görüntüleme → ~200 istek/yönetici, pay **1,5×** | **1 istek/görüntüleme** → oturum başına **300 sekme görüntülemesi**/10 dk; 20 görüntülemelik gerçekçi yük için pay **15×** |
> | `adminFloodLimit = 3000` | 10 yönetici × 20 görüntüleme × **10 parça** = 2000, + 40–60 giriş ≈ **2060** → pay **1,46×** | 10 × 20 × **1** = 200, + 40–60 giriş ≈ **260** → pay **11,5×** (⚠️ *"15×"* yanlıştı: yükü 260 ile, payı 200 ile hesaplıyordu — **iki farklı payda**; 3000/260 = 11,54) |
> | `sessionGate`'in koruduğu iş | *"bugün BOŞ"* | artık dolu: 5 rota, hepsi `Protect()` grubunda (`TestPanelSections_EveryOneIsRoutedAndBehindTheGate` iki yönde de kanıtlıyor) |
> **ÜÇÜ DE DEĞİŞTİRİLMEDİ** (brief gereği; `TestPanelConstants_ShippedValuesArePinned`
> hâlâ yeşil). **Eşik yazılıyor:** 300, görüntüleme başına **≥15 isteğe** çıkıldığında
> bağlayıcı olur (300 ÷ 20 görüntüleme). HTMX parçaları geldiğinde **yeniden sayılmalı**
> — sayı bugün değil, o gün dar.

---

## M6-03 — Transactions sekmesi

- **Bağımlılık:** M6-02 · M5-09
- **Commit:** `feat(dashboard): add transactions view`

**Amaç.** Günün işlemlerini docket kartları olarak göstermek.

**Kabul kriterleri.**
- Filtreler: tarih, lokasyon, departman, çalışan, verdict, channel.
- **⬅️ M6-02'den DEVRALINDI: filtre çubuğu bileşeni** (`input.css` →
  `@layer components` + `web/templates/components/`). Yukarıdaki altı filtre
  **burada** var olduğu için çubuk burada **çalışır hâlde** yazılabilir; M6-02'de
  yazılamazdı (aşağıdaki blok).
- **⬅️ M6-02'den DEVRALINDI: HTMX'i bu görev getirir** ve `adminCSP`
  genişlemesini **bu görev gerekçelendirir** (bkz. aşağıdaki blok).
- Her kart: lokasyon, isim, in/out, saat, trust, IP/GPS işaretleri, tag UID + ctr,
  kaşe damgası.
- `manual` ve `practice` kayıtlar görsel olarak ayırt ediliyor.
- Sayfalama/lazy yükleme HTMX ile; istemci state'i yok.
- Sorgular tenant filtreli ve indeksli (`(tenant_id, occurred_at DESC)`).

> **M6-02'den devir (2026-08-04, kullanıcı kararı).**
>
> **1. Filtre çubuğu.** M6-02'nin kabul kriterlerinden biriydi ve **sevk edilmedi**;
> **düşürülmedi, buraya taşındı**. Sebep ölçülebilir: M6-02'nin kendi kapsamı
> *"sekme içeriklerini YAZMA"* diyor, yani orada **filtrelenecek veri yoktu**.
> Üç sevk yolunun üçü de bu depoda daha önce **bulgu** sayıldı: (a) çağıranı olmayan
> **ölü CSS kuralı** — M6-01 A'da 18/18 kural canlıydı, ölü kural yazmak gerileme;
> (b) ekranda **işlevsiz kontrol** — panelin yapamadığını yapabilir gösterir;
> (c) *"bu tarihte kayıt yok"* diyen **uydurma sonuç** — sistemin ölçmediğini beyan
> etmek, M5-11'in kapattığı sınıfın ta kendisi. **Burada üçü de geçersiz**: altı
> filtre gerçek, veri gerçek, sonuç ölçülebilir.
>
> **2. HTMX.** M6-02 ölçtü: repoda HTMX'in **kodu yok** (11 eşleşmenin hepsi düzyazı),
> ağ erişilebilir olmasına rağmen **vendor'lanmadı**, çünkü iskeletin **takas edilecek
> parçası yoktu** — sekmeler düz `<a href>`. Vendor'lamak `adminCSP`'yi
> **`default-src 'none'`**'dan bir **script** için genişletmeyi zorlardı ve
> `adminlogin.go` tam tersini savunuyor. **Bu görev ilk gerçek fragment'i getiren
> taraf**, dolayısıyla HTMX'i **bu görev vendor'lar** (CDN YOK — `web/static/`'e
> gömülür, `web.Static()`) ve CSP genişlemesini **bu görev, sürümüyle ve ne kadar
> genişlediğiyle birlikte** yazar. ⚠️ `'unsafe-inline'` kabul edilmez.
> **Ağ hazır:** `TestPanelScreens_LoadNoScriptAndReachNoThirdParty` ilk `<script>`
> etiketinde **kırmızıya döner** — yani CSP'yi genişletmek **görünür** bir edit olmak
> zorunda, sessizce miras alınamaz.
>
> **3. Bütçe.** M6-02 ölçtü: bir sekme görüntülemesi = **1 ücretli istek**
> (300 × `200`, 301.'de `429`). `adminFloodLimit`/`adminSessionLimit` türetmesi
> **~10 parça/görüntüleme** varsayıyor. **Parçalar bu görevle geliyor → sayıyı bu
> görev yeniden saymalı.** Eşik yazılı: `adminSessionLimit = 300`, görüntüleme başına
> **≥15 isteğe** çıkıldığında bağlayıcı olur.

> **Kart düzeltmesi (2026-08-05, M6-03 uygulaması sırasında).**
>
> **Kartın kriterleri doğruydu; eksik olan tek şey okuma yolunun HİÇ var olmamasıydı.**
> Ölçüm: `db/queries/transactions.sql`'deki beş sorgunun **beşi de** tap YAZMA
> yolunun (`GetLastOpenTransaction`, `LockEmployeeForTap`,
> `SecondsSinceLastRecordedTap`, `GetLastTransactionForEmployee`,
> `InsertTransaction`) — **listeleme sorgusu yoktu**, yani bu görev okuma yolunu
> sıfırdan yazdı. Kartın *"indeksli `(tenant_id, occurred_at DESC)`"* kriteri ise
> **zaten karşılanmıştı**: `transactions_tenant_occurred_idx` `00005:173`'te var ve
> `EXPLAIN` onu kullanıyor (`Index Scan using transactions_tenant_occurred_idx`,
> 151.970 satırlık dev DB'de). **Migration YAZILMADI ve gerekmedi.**
>
> **Üç şey kartın söylemediği şekilde çıktı ve yazılıyor:**
>
> **1. Filtre çubuğunun VIEW TİPLERİ `components`'a girmek zorunda kaldı.** Kart
> bileşenin `web/templates/components/`'e gitmesini istiyor ve gitti — ama `pages`
> zaten `components`'ı **import ediyor**, dolayısıyla `pages` tipini alan bir bileşen
> **import döngüsü** kapatıyordu. Çözüm deponun kendi kalıbı: `components.Tab` de
> orada tanımlı — bileşen kendi girdisinin şeklini sahiplenir. `DocketView`,
> `FilterBarView`, `OptionView` bu yüzden `components` içinde.
>
> **2. 🔴 HTMX'i `web/static/js/`'e vendor'lamak TAILWIND'İ ZEHİRLEDİ.**
> `tailwind.config.js`'in `content` deseni `./web/static/js/**/*.js` içeriyordu ve
> Tailwind her içerik dosyasını **ham metin** olarak tarıyor → 51 KB'lık minified
> kütüphanenin **kendi iç dizgeleri** üç ölü kural doğurdu: `.ease-in`, `.resize`,
> `.transition`. **Kontrollü deneyle kanıtlandı** (htmx hariç tutulup yeniden
> derlendi: üçü de kayboldu). `content` artık `'!./web/static/js/*.min.js'` taşıyor —
> htmx'i adıyla değil, **minified vendor deseni** olarak dışlıyor, ki bir sonraki
> kütüphane de kapsansın. Kendi düzyazımdan doğan dört ölü kural daha vardı
> (`.contents`, `.invisible`, `.opacity-80`, `.ring`, sonra `.outline`) — skill
> `tappa-brand`'in *"yorumda utility'yi tarif et, yazma"* kuralı, ve bu görevde
> **beş kez** ateşledi. **Sevk edilen ölü kural: 0.**
>
> **3. 🔴 AÇIK `tenant_id` YÜKLEMİNİ HİÇBİR DAVRANIŞ TESTİ KORUMUYORDU.** Mutasyon
> M15: `ListPanelTransactions`'tan `t.tenant_id = @tenant_id` **silindi** ve uçtan
> uca çapraz-tenant testi **YEŞİL KALDI** — çünkü RLS tek başına yabancı satırları
> zaten engelliyor. Bu, CLAUDE.md §6'nın uyardığı şeyin **ters yönü**: davranış
> testi iki savunmanın yalnız **birleşimini** görebilir, biri ayakta olduğu sürece
> hiçbir şey sızmaz. §4.5 **iki** savunma istiyor, o yüzden ikincisine kendi ağı
> yazıldı: `internal/domain/ledger/query_test.go` SQL metnini okuyup her panel
> sorgusunun kendi **kapsam sütununu** (`tenants` için `id`, diğerleri için
> `tenant_id`) çağıranın parametresine bağladığını doğruluyor. M15 bu ağla
> **KIRMIZI**. ⚠️ Dürüst kayıt: bu test ürünü tek başına güvenli yapmaz — satırları
> durduran RLS'tir; yaptığı şey **ikinci savunmayı kırmızı test olmadan
> silinemez** kılmak.
>
> **📏 CSP — GERÇEK TARAYICIDA ÖLÇÜLDÜ, dört kollu deney (Chrome headless, ürünün
> KENDİ vendor'lanmış htmx'i, sunucu tarafında sayılan istekler):**
>
> | # | Politika | script çekildi | fragment çekildi |
> |---|---|---|---|
> | 1 | base (`default-src 'none'`, script-src YOK) | **0** | **0** |
> | 2 | base + `script-src 'self'` | **1** | **0** |
> | 3 | base + `script-src` + `connect-src` (**SEVK EDİLEN**) | **1** | **1** |
> | 4 | hiç CSP yok (kontrol) | 1 | 1 |
>
> Yani **iki direktifin ikisi de taşıyıcı**: `script-src` olmadan htmx **hiç
> yüklenmiyor**; `connect-src` olmadan htmx **yükleniyor ama XHR'ı sessizce
> bloklanıyor** (kol 2 — sayfalama düğmesi hiçbir şey yapmaz). Kol 3, CSP'siz
> kontrolle (kol 4) **birebir aynı** sonucu veriyor → fazlası gerekmiyor.
> **`'unsafe-inline'`/`'unsafe-eval'` YOK.** Genişleme **6 → 8 direktif**, ve
> **yalnız bir URL** (`/admin`) genişlemiş politikayı gönderiyor; fragment
> (`/admin/dockets`) **base** politikayı alıyor (gerçek binary'den doğrulandı).
>
> **📏 BÜTÇE — YENİDEN SAYILDI: ÇARPAN GERİ GELMEDİ.** Gerçek sunucu + gerçek
> Postgres + gerçek oturum, M6-02'nin yöntemiyle: **300 servis edildi, ilk `429`
> tam #301'de** → **görüntüleme başına 1,000 ücretli istek**, değişmedi. Filtre
> değişimi de 1 (çubuk düz GET formu, fragment değil). Yeni olan **ikinci bir
> payda**: bir *gün-yürüyüşü* = `ceil(N / PageSize)` istek. Gerçek dağılıma karşı
> (40.850 tenant-günü): medyan **1**, p95 **1**, p99 **2** istek; 300'ü aşan üç
> tenant-günü var ve **üçü de bu deponun kendi test suite'inin** yazdığı günler,
> işletme günü değil. **≥15 eşiğine** ancak tek bir filtresiz görünüm **≥350 kayıt**
> tuttuğunda varılıyor (**13 basış** "show more" = `ceil(350/25)`=**14 istek**: 1
> belge + 13 fragment; **basış sayısı istek sayısından bir eksiktir** ve bu satırın
> ilk hâli istek sayısını basış etiketiyle basıyordu). **Sabit DEĞİŞTİRİLMEDİ**; türetme
> `adminratelimit.go`'da paydalarıyla birlikte güncellendi. ⚠️ Değişen şey şu:
> M6-03 öncesi bir oturum görüntüleme başına 1 isteği **hiçbir kullanıcı eylemiyle**
> aşamıyordu; artık sayfalamayla aşabilir. Kaldıraç `adminSessionLimit` değil
> **`ledger.PageSize`** (25) — ikiye katlamak tablodaki her sayıyı yarıya indirir.
>
> **§4.7 — ekrana ne çıkıyor:** `policy_context` ölçüldü, **koordinat taşımıyor**
> (kapalı anahtar kümesinde `tap:gpsDistanceM` var, `lat`/`lng` yok — migration 0008
> zaten böyle tasarlamış). Ama `transactions` satırının **kendisi** `gps_lat`,
> `gps_lng`, `source_ip` taşıyor, o yüzden **üçü de SELECT'e alınmadı**: sorgu
> seçmiyor, `ledger.Record`'da alan yok, `components.DocketView`'da alan yok — **üç
> bağımsız duvar**. Ekranda **işaret** var: `IP yes/no/n-a · GPS yes/no/n-a`
> (skill `tappa-brand`'in adisyon krokisindeki `IP ✓ GPS ✓`). **Üç durum, iki değil**
> — `NULL` = *"bu kanalda değerlendirilmedi"*, ve manuel kaydı *"no"* diye göstermek
> onu koşulmamış bir kontrolde başarısız ilan etmek olurdu. **`source_ip` DE
> gösterilmiyor** (kart *"işaret"* diyor; ham adres çalışanın cihaz adresidir,
> mekânın statik IP'si değil). 25 gerçek kayıt render eden sayfada koordinat-şekilli
> değer taraması: **cursor zaman damgaları maskelendikten sonra SIFIR** (maskesiz tek
> eşleşme `40.605278`'di ve o `after_at`'in saniye.nanosaniyesiydi).
>
> **Sayılar:** `make test` **1647 → 1674 PASS**, 0 SKIP, 0 FAIL, **14 → 15 paket**
> (yeni `internal/domain/ledger`). `app.css` **18.376 → 20.443 bayt**, 14 yeni
> seçici, **hepsi gerçek bir `class=`'a iz sürüyor**. Mutasyon: **15/15 kırmızı**
> (M15 ancak yukarıdaki yeni ağ yazıldıktan sonra).

> **Kart düzeltmesi 2 (2026-08-05, 3. tur denetiminden sonra).**
>
> **Denetçi 23 mutasyon koştu, 6'sı hayatta kaldı, ikisi bloklayan — ve altısı da
> aynı sınıftı: bir YORUM, testin ölçmediği bir şeyi *"asserted"* diye ilan
> ediyordu.** Bu, M6-02'de beş kez ısıran hatanın aynısı ve bu sefer **onu düzelten
> commit'in içinde** işlendi. Altısı da kapatıldı; hepsi mutasyonla kanıtlandı.
>
> | Bulgu | Neydi | Şimdi |
> |---|---|---|
> | **F1** (bloklayan) | *"genişleme tek URL'de ve **bir testle pinli**"* — test yalnız **sayfa-başına karşılıklılık** ölçüyordu; beş bölümün beşine de script verilince paket **YEŞİL** | **kardinalite pinlendi** (genişletilmiş CSP gönderen URL sayısı **tam 1**, ve o URL transactions olmalı) · `M-G2` → **RED** |
> | **F2** (bloklayan) | `input.css` *"floor is asserted by `TestPanelScreens_TouchTargetClassesReserve44px`"* diyordu ama `panelTouchTargets` haritasında **`filter-input` yoktu**; `min-h-11` silinince paket **YEŞİL** | `filter-input` + `min-h-11` haritada · **yeni `TestPanelScreens_FormControlsCarryATouchTargetClass`** (`<a>`/`<button>` taraması `<select>`/`<input>`'a hiç ulaşmıyordu) · `M-U` → **RED** |
> | **F3** | belt testi **tam sözdizimi değişim dedektörü**: ters yazım yanlış alarm; `OR TRUE`, JOIN'e taşıma, `queryBody` körleştirme, **sabit liste** → dördü de geçiyordu | **özellik** testine çevrildi: sorgunun KENDİ `WHERE`'ünde **üst-seviye conjunct** olarak kapsam eşitliği; liste `ledger.go`'dan **türetiliyor**; `queryBody` için ayrı sınır testi · `M-F3a/b/c/d` → **dördü de RED** |
> | **F4** | Tailwind düzeltmesinin **ağı yoktu** ve `!*.min.js` deseni **iki şekilde yeniliyordu** (alt dizin · minified adı taşımayan vendor) | vendor **taranan ağacın dışına** taşındı (`web/static/vendor/`), desen yerine **dizin** kural oldu; **`TestTailwind_ScansNoMinifiedSource`** özelliği tutuyor: globların eşleştiği hiçbir dosya **makine üretimi görünemez** (ölçüldü: `tap.js` en uzun satır **83**, `htmx.min.js` **51.238** — eşik 400) · `M-F4` → **RED** |
> | **F5** | `coordinateField` regexp'i körleştirilince §4.7 tip duvarı testi **YEŞİL** | **negatif kontrol eklendi** (`TestCoordinateScanner_RejectsTheThingsItExistsToReject`) · `M-F5` → **RED** |
> | **F11** | `/admin/dockets`'in kapı arkasında olduğunu **açıkça** doğrulayan test yoktu (tablo taraması onu görmüyor) | **`TestDocketFragment_IsBehindTheSessionGate`** · `M-F11` → **RED** |
>
> **Ayrıca düzeltildi:**
> **F7 — boş durum sistemin ölçmediğini beyan ediyordu.** *"**Nobody tapped a
> plaque on this day.**"* §5 satır 3 gereği **yanlış**: oturumu olmayan biri plakete
> dokunduğunda **kayıt yazılmaz**, aktivasyona gider — yani binadaki her yeni
> başlayan dokunsa bile sayfa aynen böyle boş çıkar. *"Nothing was recorded here on
> this day."* oldu (başlık zaten dürüsttü). **M5-11'in kapattığı sınıf.**
> **F8 — sorgu dört sütunu seçip hiç okumuyordu** (`entered_by` — bir **yöneticiyi**
> tanımlayan id — artı üç join girdisi id'si). §4.7 savunmasının tamamı *"seçilmeyen
> bir sütun şablona ulaşamaz"* üzerine kurulu; okuyucusu olmayan dört sütun onu
> gevşetiyordu. **Satır 20 → 16 alan.**
> **F9 — `loadZone` hatayı sessizce yutuyordu** (§7). Artık `slog` ile raporlanıyor:
> tzdata'sız bir konteyner **çalışan bir panel gibi görünüp** her damgayı sessizce
> kaydırırdı.
> **F10 — test adı ve başlık yorumu yalan söylüyordu.** `TestPanelScreens_LoadNoScript…`
> hâlâ *"panelde script YOK · HTMX vendor'lanmadı · ilk `<script>`'te kırmızıya
> döner"* diyordu — **üçü de** gövdeyi yeniden yazan commit'te yanlışa döndü.
> `TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty` oldu, yorum yeniden yazıldı.
>
> ⚠️ **`internal/domain/ledger/query_test.go`'de bir düzenleyici `''`'yi `”` (U+201D)
> yapmıştı** — ADR 0002'nin normatif SQL'ini alıntılayan satırda, yani tam olarak
> birebir olması gereken yerde. ASCII'ye döndürüldü; dokunulan tüm dosyalar tarandı,
> başka akıllı tırnak **yok**.
>
> **📏 F6 — FİLTRE ÇUBUĞU SINIRSIZ BÜYÜYOR. KULLANICI KARARI GEREKİYOR; KARAR
> VERİLMEDİ.** Canlı ölçüm (gerçek binary, gerçek oturum, KF tenant'ı, 2026-08-04):
>
> | | bayt | payda |
> |---|---|---|
> | tam sayfa | **867.233** | — |
> | çalışan `<select>`'i **tek başına** | **835.319** | sayfanın **%96'sı** |
> | seçimler hariç her şey (25 docket kartı dahil) | **30.462** | — |
> | sayfalama fragment'i (filtre çubuğu **yok**) | **27.781** | — |
>
> **7.490 `<option>`**, çünkü `ListEmployeesForTenant` **LIMIT taşımıyor**. Filtre
> çubuğu sayfanın geri kalanının **27,5 katı**. `Cache-Control: no-store` → **her
> görüntülemede ve her filtre değişiminde** yeniden iniyor. Ölçülen birim maliyet:
> **`<option>` başına 111,5 bayt**.
>
> | çalışan | employee select | sayfa (yaklaşık) |
> |---|---|---|
> | 50 | 5 KB | 35 KB |
> | 250 | 27 KB | 57 KB |
> | 500 | 54 KB | 84 KB |
> | 1.000 | 109 KB | 139 KB |
> | 2.500 | 272 KB | 302 KB |
>
> Sayfa **100 KB altında ~645 çalışana**, **250 KB altında ~2.022 çalışana** kadar
> kalıyor. ⚠️ *(Bu DB'deki 7.489 çalışanın ezici çoğunluğu test kirliliği — bir
> tenant'ta **76.618** bile var. Gerçek müşteride bugün küçük; bulgu **yapının
> sınırsız büyümesi**.)* ⚠️ **DÜZELTME (6. tur): o 76.618 YANLIŞ.** `GROUP BY
> t.name` ile ölçülmüştü ve *"Kebab Factory Ltd"* adı **44.752 farklı tenant**
> tarafından paylaşılıyor → sayı tek bir kadro değil, **tenant'lar arası toplam**.
> Doğrusu (`GROUP BY tenant_id`): **en büyük tek tenant 7.669**, ikinci **31**,
> tüm tenant'lar toplamı **111.167 / 64.838 tenant**.
>
> 🔴 **VE BU, BÜTÇE YENİDEN-SAYIMININ GÖRMEDİĞİ EKSEN.** `adminratelimit.go`
> **istek ADEDİNİ** doğru saydı (1/görüntüleme) ve **bir isteğin MALİYETİNİ** hiç
> saymadı. İki ayrı payda; sayı doğruydu, kapsam değil.
>
> **Üç okuma, ölçülmüş bedelleriyle — kullanıcı seçecek:**
> **(a) Select'i sınırla.** Ölçüldü: `status='active'` **6.817** kişiye indiriyor
> (%9 azalma — bu tenant'ta **işe yaramıyor**), *"son 90 günde işlemi olan"* **7.464**
> (%0,3 — hiç yaramıyor). Yani bu veri kümesinde **hiçbir ölçüt kurtarmıyor**; işe
> yarayan tek biçim sabit bir tavan (ör. ilk 200). **Bedeli:** listede olmayan biri
> **panelden filtrelenemez** — ve bu, sorgunun kendi yorumuyla doğrudan çelişir
> (*"EVERY STATUS IS RETURNED, deactivated included"*, gerekçesi §4.6: işten
> ayrılmış birinin kayıtları erişilebilir kalmalı). Sessiz kırpma **en kötüsü**.
> **(b) Kontrolü değiştir** — `<select>` yerine metin girişi + sunucu tarafı isim
> eşleşmesi (`ILIKE`). Sayfa **867 KB → ~31 KB** (**%96 düşer**), ölçek sorunu
> tamamen kalkar. **Bedeli:** keşfedilebilirlik biter (adı bilmen ve doğru yazman
> gerekir), yeni bir sorgu + muhtemelen bir indeks, ve *"kim çalışıyor"* listesi
> Employees sekmesine (M6-05) taşınır.
> **(c) LİMİT olarak yaz, bugün dokunma.** **Bedeli:** ~645 çalışanın üstündeki bir
> tenant her görüntülemede >100 KB indirir; ~2.000'in üstünde >250 KB. Tek VPS'te
> ölür değil ama **monoton büyüyor** ve `no-store` yüzünden hiç önbelleklenmiyor.
>
> **Şu an yürürlükte olan: (c)** — çünkü (a) ve (b) **ürün kararı** ve bu kart onları
> almaya yetkili değil. Sayı burada, karar kullanıcının.

> **Kart düzeltmesi 3 (2026-08-06) — F6 KARARA BAĞLANDI: (b) metin girişi.**
>
> **Kullanıcı (b)'yi seçti** (2026-08-06). Uygulandı, ölçüldü, mutasyonla kanıtlandı.
>
> **📏 KAZANÇ — ETİKETLERİYLE, aynı tenant, aynı gün, gerçek binary:**
>
> | | ÖNCE | SONRA | |
> |---|---|---|---|
> | **tam sayfa** (belge) | 867.233 B | **32.066 B** | **−%96,3 · 27,0× küçük** |
> | bunun içinde **tüm `<select>`'ler** | 836.771 B | 1.452 B | |
> | bunun içinde **çalışan `<select>`'i** | 835.319 B | **0 B** (artık `<input>`) | |
> | `<option>` sayısı | 7.510 | 20 | |
> | **docket kartı** (yük) | 25 | 25 | değişmedi |
>
> ⚠️ **Paydalar farklı ve yazılı:** *"sayfa baytı"* belgenin tamamı; *"filtre çubuğu
> baytı"* içindeki `<select>`'ler. İkisini karıştırmak, %96'yı hiç %96 olmayan bir
> şeye atfetmek olurdu. ⚠️ **Mutlak sayılar bugünkü gerçek müşteriyi ABARTIYOR:** o
> tenant'ta **7.669** çalışan var ve ezici çoğunluğu bu deponun test kalıntısı.
> ⚠️ **Bu cümlenin ilk hâli *"aynı DB'de 76.618'lik başka bir tenant var"* diyordu
> ve HİÇBİR tenant için doğru değildi** — `GROUP BY t.name` bir isim altındaki
> **44.752 tenant'ı** topluyordu. Doğru ölçüm (`GROUP BY tenant_id`): en büyük
> **7.669**, **ikinci en büyük 31**, toplam **111.167 / 64.838 tenant**. Kaydın
> dürüstlüğü bundan **güçlenerek** çıkıyor: ölçümler tam da o 7.669'luk seed
> tenant'ında alındı ve ikinci gerçek işletme **31 kişilik**. Bulgu **şekil**,
> büyüklük değil.
>
> **🔴 PERFORMANS RAKAMLARI GERİ ÇEKİLDİ (6. tur) — YENİDEN ÜRETİLEMEDİLER.**
> Bu blok *"JOIN filtresi 14.303 ms · CTE 527 ms · **27×** · **95×**"* diyordu.
> Denetçi yeniden üretemedi; ben de üretemedim. **Sebep ölçüldü:** geliştirme
> veritabanı **hiç ANALYZE edilmemişti** — `pg_stat_user_tables`'ta `last_analyze`,
> `last_autoanalyze`, `last_autovacuum` **üçü de NULL** ve `n_live_tup` **5.326**
> derken tablo **111.167** satır taşıyor. Planlayıcı, gerçeğin ~20 katı küçük bir
> istatistikten plan seçiyordu. 14,3 sn'lik `EXPLAIN` çıktısı **gerçekti**; ondan
> çıkarılan *"bu şekil 27× yavaş"* **sonucu** gerçek değildi.
>
> **`ANALYZE` sonrası, aynı tenant, aynı gün, sıcak önbellek, 5 tekrar:**
>
> | şekil | süre | employees taraması |
> |---|---|---|
> | **JOIN filtresi** (değiştirdiğim şekil) | **2,0–2,2 ms** | `employees_pkey`, `loops=318`, her biri 0,003 ms |
> | **`CTE AS MATERIALIZED`** (sevk edilen) | **7,6–10,0 ms** | `loops=1` |
> | `CTE AS NOT MATERIALIZED` | 7,7–17,7 ms | `loops=1` |
>
> Yani **doğru istatistikle değiştirdiğim şekil ~4× DAHA HIZLI**, ve
> `MATERIALIZED` ile `NOT MATERIALIZED` **ayırt edilemiyor**. **27× ve 95× geri
> çekildi.**
>
> **Peki neden CTE kalıyor?** Hız için değil — bu veride **~6 ms'e mal oluyor** —
> **başarısızlık biçimleri farklı olduğu için**: CTE'nin maliyeti **sınırlı**
> (employees **bir kez** taranır → `O(employees) + O(gün satırları)`), JOIN
> filtresininki **planlayıcıya bağlı** ve bu depoda o planlayıcının
> `O(gün satırları × employees)` seçip **14 sn** harcadığı **ölçülmüş bir vaka**
> var — oturum başına 300 istekli, tap yüzeyiyle **aynı havuzu** paylaşan bir
> yüzeyde. Bir kez gözlenmiş bir kuyruğu kesmek için 6 ms ödemek; **katılmayan
> bilerek geri alabilsin diye yazıldı.**
>
> ⚠️ **LİMİT: `MATERIALIZED`'ı hiçbir test korumuyor.** Kelimeyi silmek tüm suite'i
> **yeşil** bırakıyor, çünkü mevcut istatistiklerle planlayıcı iki hâlde de
> `loops=1` üretiyor — yani yokluğu hiçbir davranış testine görünmüyor, ve kelimeyi
> **grep'leyen** bir test ağ değil **değişiklik dedektörü** olurdu. Buradaki
> garanti bir **en-kötü-hâl argümanı**, pinlenmiş bir invaryant değil.
>
> **İNDEKS EKLENMEDİ ve gerekmiyor** (baştan joker'li `ILIKE` btree kullanamaz;
> daha hızlısı `pg_trgm` + GIN = **uzantı + migration**, bu görevin işi değil).
> İstek başına tek tarama 7.669 çalışanda **~7 ms**.
>
> **§4.5:** eşleşme sorgusu `WithTenant` içinde koşuyor ve **CTE'nin KENDİ**
> `tenant_id` yüklemi var. Kuşak ağı bunu **otomatik aldı** — liste `ledger.go`'nun
> `q.X(` çağrılarından türetiliyor — ama ağ **iki yerde genişletilmek zorunda kaldı
> ve ikisi de ölçümle bulundu:** (1) `whereClause` yalnız **ilk** WHERE'i okuyordu,
> CTE gelince bu **yanlış cümleciği** denetler oldu → artık **her** WHERE bloğu
> denetleniyor (CTE dahil — *daha güçlü*); (2) alias deseni `[a-z_]+` **rakam
> içeren** alias'ı (`e2.`) reddediyordu ve **doğru kapsanmış** bir CTE'yi kapsamsız
> ilan etti → `[a-z_][a-z0-9_]*`. İkisi de negatif kontrole eklendi.
>
> **§4.6:** eşleşmede **`status` yüklemi YOK ve olmamalı** — kısa listeleme
> seçenekleri tam olarak bunun için reddedildi. Ağ:
> `TestPanelTransactionsDB_NameFilterFindsPeopleWhoHaveLEFT` (fixture gerçekten
> `deactivated` mı, önce o doğrulanıyor).
>
> **Kaçak kapatıldı:** `%`/`_` **ILIKE joker'i**; kutuya `100%` yazan biri sessizce
> **herkesi** seçerdi. Kaçış **sorgu katmanında** (`escapeLike`), handler'da değil —
> ilk hâli handler'da kaçırıyordu ve **kaçırılmış dize kutuya geri basılıyordu**,
> yani her gönderim kaçışı bir kez daha kaçırıyordu. Artık handler/görünüm/URL
> boyunca **yazılan neyse o** taşınıyor.
>
> **Mutasyon (7/7 kırmızı, hepsi uygulandığı doğrulanmış):** CTE'den tenant yüklemi
> sil · ana sorgudan sil · ayrılmışları ele · eşleşmeyi `OR TRUE` yap · kaçışı
> kaldır · kaçışı handler'a geri taşı · roster'ı `<select>` olarak geri getir.
>
> ⚠️ **Bir test artefaktı bulundu ve düzeltildi:** çapraz-tenant testi tüm belgeyi
> tarıyordu; isim kutusu **yazılanı geri bastığı** için B'nin çalışan adını arayınca
> kendi tuş vuruşunu bulup *"§4.5 ihlali"* diyordu. Ölçüldü: **0 docket**, kart
> içinde **0** eşleşme, B'nin plaketi ve mekânı **yok** — sayfadaki tek eşleşme
> `value="…"`. Id filtreleri **tüm belgede** denetlenmeye devam ediyor (yabancı bir
> mekân adı asla yansıtılmaz), isim filtresi **docket'larda**. Ayrıca yansımanın
> **kaçırıldığı** (`<script>` ham geçmiyor) artık pinli.

> **Kart düzeltmesi 4 (2026-08-06, 6. tur denetiminden sonra).**
>
> Denetçi **24 mutasyon** koştu (hepsi uygulandığı doğrulanmış), **21 kırmızı**,
> mekanizmayı sağlam buldu — **dört bloklayan + performans rakamlarının yeniden
> üretilememesi** dışında. Hepsi kapatıldı; **ikisi bu oturumun kendi derslerinin
> tekrarıydı ve ikisi de benim düzeltme turumun içinde doğdu.**
>
> | Bulgu | Neydi | Şimdi |
> |---|---|---|
> | **F1** | `adminlogin.go` **var olmayan bir testi** canlı bekçi diye gösteriyordu (`TestPanelScreens_LoadNoScript…` — **3. turda onu ben yeniden adlandırdım**, `grep -c 'func …'` → **0**). ⚠️ Aynı cümlenin ikizini `dashboard_test.go`'da düzeltip **üretim kaynağındakini bıraktım** | gerçek ağ adlandırıldı **ve ne yaptığı/YAPMADIĞI yazıldı** (*"ilk `<script>`'te kırmızıya dönmez"* — karşılıklılık + kardinalite) |
> | **F2** | *"adminCSP **beş** kaynak; bu **yedi**"* — **iki mutlak sayı da yanlış** (delta doğru) | **sayıldı**: `adminCSP` **6**, scripted **8**. İki yerde düzeltildi |
> | **F3** | *"başka bir tenant'ta **76.618**"* — hiçbir tenant için doğru değil | `GROUP BY t.name`'in **44.752 tenant'ı** topladığı **yeniden üretildi**; doğrusu **max 7.669 / ikinci 31 / toplam 111.167** |
> | **F4** | kuşak ağı `os.ReadFile("ledger.go")` ile **tek dosya** okuyordu; yorumu *"paketin kendi kaynağı"* diyordu | `filepath.Glob("*.go")` + `_test.go` hariç · **pozitif kontrolle kanıtlandı**: aynı kapsamsız çağrı kardeş dosyada artık **RED** |
> | **N1** | `s[:100]` **BAYT** kesiyordu → çok baytlı runeyi bölüp **500** üretiyordu (99 ASCII + `é`; ayrıca NUL) | rune sınırında kesim + NUL temizliği + `ToValidUTF8` · **11 düşmanca girdi** testi · mutasyon **RED** |
> | **N3** | fragment `renderScripted`'a çevrilince **hiçbir test kırmızıya dönmüyordu** (kardinalite ağı yalnız `PanelSections`'ta geziyor) | **`TestDocketFragment_UsesTheUnwidenedPolicy`** + `adminlogin.go`'da *"URL"* → *"SECTION"* |
> | **N4** | *"**14** kez show more"* — o **istek** sayısı, basış **13** | iki yerde düzeltildi, aritmetiği yazılı |
> | **N5** | `web/static/js/README.md` üç yorumda anılıyor, **dosya yok**; README'nin başlığı da bayat | üç yorum + başlık düzeltildi; vendor/ownership ayrımı README'ye yazıldı |
> | **N6** | `OptionView.Group` hâlâ *"employee's lifecycle status"* diyordu | düzeltildi (çalışan artık liste değil) |
> | **N7** | gün/zon satırı **mono değildi** | **mono yapıldı** — skill `tappa-brand`: *"bir veri hücresi mono değilse yanlıştır"*; bu satır **tarih + zaman dilimi** taşıyor, yani docket'lardaki saatlerle **aynı sınıf**. Bölümün gerçek başlığı `PanelShell`'deki `<h1>` ve o display face'te kalıyor |
>
> ⚠️ **Bu oturumda sayı-etiketi hatası artık YEDİ kez çıktı** ve yedincisi (F3)
> **benim**: `GROUP BY t.name` ile ölçülen bir toplamı *"tek tenant'ın kadrosu"*
> diye yazdım. Bu turda düzelttiğim **her sayı** kendi komutuyla yeniden ölçüldü —
> direktifler sayıldı, tenant başına **max** ile **toplam** ayrı ayrı sorgulandı,
> test adı `grep -c 'func …'` ile doğrulandı.

> **Kart düzeltmesi 5 (2026-08-06, 8. tur — `tappa-security-auditor` ONAY).**
>
> Güvenlik denetimi **onayladı**: §4.5 yedi vektörlü çapraz-tenant saldırısı
> (B'nin id'leri, **B'nin tablosundan okunmuş gerçek cursor**, fragment rotasına
> doğrudan) → **hepsi 200, docket içinde 0 sızıntı** · §4.7 üç duvar **koordinat
> taşıyan gerçek satırlara** karşı · §4.3 rol düzeyinde `UPDATE=f DELETE=f` ·
> **SQL enjeksiyonu yok** (15 vaka) · **XSS yok** (12 payload × 4 yankı noktası,
> depolanmış yol dahil) · htmx upstream ile **`cmp` birebir** · CSP telde **6/8**,
> kardinalite **tam 1**, fragment **genişletilmemiş**.
>
> 🔴 **K1 — SINIFIN SEKİZİNCİ TEKRARI VE YİNE BENİM: "düzelttim" dediğim dört şey
> ağaçta yoktu.** Sebep mekanik ve utanç verici: N4/N5/N6 düzeltmelerim **tek bir
> python betiğindeydi ve betik N4'ün `assert`'inde (em-dash uyuşmazlığı) düştü** —
> N5 ve N6 satırlarına **hiç ulaşmadı**. Ben betiğin **niyetini** rapor ettim,
> **çıktısını** değil. Ölçülen hâl: N4 **2'de 1**, N5 **0'da 3**, N6 **0'da 2**.
> **En ağırı:** `dashboard_test.go:396` *"vendored into `web/static/js/`"* diyordu —
> **F4'ün yapısal düzeltmesinin tam tersi**; vendor'u `vendor/`'a taşımanın tek
> sebebi Tailwind'in taradığı ağacın dışına çıkarmaktı, ve o yorumu okuyup bir
> sonraki kütüphaneyi `js/`'e koyan kişi F4'ü geri açardı. **Dördü de düzeltildi ve
> bu kez her biri `grep` çıktısıyla doğrulandı** (stale referans: **0**).
>
> 🔴 **K2 — N1'in RUNE yarısı korumasızdı ve kart *"mutasyon RED"* diyordu.**
> Denetçi bayt kesimini geri koydu → **tam paket YEŞİL**. Mekanizma yeniden
> üretildi: aynı düzeltmede eklediğim `ToValidUTF8` yarım rune'u **siliyor**, yani
> 500 hiç oluşmuyor ve *"500 değil"* diye bakan test onu **göremiyor** — yalnız NUL
> yarısı pinliydi. **No-op değil, ölçüldü:** 108 rune'luk bir ad sevk edilen kodda
> **100 rune**, mutasyonda **50 rune** → her Malta/Türkiye adı için filtre yazılanın
> **yarısını** kullanır ve yine makul bir sayfa döner. **Yeni ağ:**
> `TestEmployeeFilter_TruncatesByCharacterNotByByte` (ċ ġ ħ ż · İ · ASCII kontrol
> kolu). Mutasyon **M-G → RED** (uygulandığı sha256 ile doğrulanmış).
>
> 🔴 **K4 — sha256 kaydediliyordu, hiçbir mekanizma okumuyordu.** Cevap *"disiplin"*di.
> **Yeni ağ:** `TestVendoredScript_MatchesTheRecordedDigest` — hash'i **README'den
> okuyup** gömülü dosyaya karşı yeniden hesaplıyor (ikinci bir kopya tutmuyor;
> ikisi olsa kayarlardı). Mutasyon: htmx'e **tek bayt** eklendi → **RED**.
> *Ne iddia etmiyor:* tedarik zinciri provenansı değil — dosyanın, birinin hash
> yazdığı dosya olduğunu kanıtlar, yani yükseltme **baytları ve kaydı tek edit'te**
> değiştirmeye zorlanır.
>
> **📏 K3 — oran sınırı türetmesi kendi açtığı ekseni yarım bırakıyordu** (bayt
> cevaplanmış, **DB zamanı** cevapsız; tavanlar hâlâ **3,0–5,7 ms**'in üzerine
> kuruluydu ve o rakam panelin **sorgusu olmadan önce** ölçülmüştü).
> **Ölçüldü** (`EXPLAIN ANALYZE`, 7.769 çalışanlı tenant, sıcak, 3 tekrar, ANALYZE
> sonrası) — **payda: sayfa isteği başına**, oturum başına değil:
>
> | | sıradan gün (1.674) | en büyük gün (13.458) |
> |---|---|---|
> | isim filtresi **yok** | **0,6–0,8 ms** | **0,6–0,9 ms** |
> | isim, çok eşleşme | 9,7–17,2 ms | **20,3–37,5 ms** |
> | isim, az eşleşme | 8,8–14,8 ms | 9,3–11,8 ms |
> | isim, hiç eşleşme | 8,5–14,6 ms | 12,7–20,2 ms |
>
> Auth maliyetiyle: **filtresiz ~4–7 ms · filtreli ~12–43 ms** istek başına.
> 300 filtreli istek ≈ **3,6–12,9 sn DB zamanı / pencere / OTURUM** (eski 3,0–5,7
> ms'in ima ettiği ~1 sn değil). Bağımsız denetçi aynı şekilde **108 ms**'e kadar
> ölçtü → üst uç **desene ve güne göre değişken**, tavan değil. **Sabit
> DEĞİŞTİRİLMEDİ:** ulaşmak **geçerli bir panel oturumu** gerektiriyor (anonim
> flood değil), yavaş kolu süren 7.769'luk kadro **test kalıntısı**, ve müdürün
> gerçekte açtığı **filtresiz sayfa 0,6–0,9 ms**. ⚠️ `sessionGate` handler'dan
> **önce** koştuğu için 429 olan istek bu sorguyu **çalıştırmıyor**.
>
> **K5 — iki sayı düzeltildi:** *"~6 ms"* → **6–8 ms** (desene göre; çok eşleşen
> desende fark **~37 ms**, hiç eşleşmeyen desende **CTE daha hızlı**), *"~7 ms"* →
> **6,5–8,6 ms** (denetçi aynı şekilde 9,8–11,2 ölçtü). ⚠️ Geri çekmenin **yönü** ve
> *"CTE'nin maliyeti sınırlı"* gerekçesi **geçerliliğini koruyor**.
>
> **K6 — iki LİMİT yazıldı:** (1) **`Referrer-Policy` başlığı yok** — bugün etkisiz
> (CSP dış köken isteği üretemiyor, `<meta name="referrer" content="no-referrer">`
> zaten var), ama panel URL'i artık **yazılan bir kişi adı** taşıyor; §4.7 ihlali
> **değil**, sertleştirme. (2) **Unicode katlama:** `ħaddiem`·`ŻAMMIT`·`istanbul`
> **çalışıyor**, ama `IŞIK` → `Işık`'ı **bulmuyor** (`lower('I')='i'≠'ı'`) ve NFC
> aramа NFD saklanmışı **bulmuyor**. **İki pazarımız Malta ve Türkiye** olduğu için
> yazıldı. **Güvenlik/§4.6 sonucu yok:** eşleşmeyen ad hiçbir şeye daralır ve
> **filtresiz gün her kaydı listelemeye devam eder** — kayıt ulaşılmaz olmuyor,
> yalnız o yazımla bulunmuyor. Düzgün çözümü normalize eden collation veya üretilmiş
> sütun = **migration**.
>
> ⚠️ **Sayılar tur turdan kayıyor** çünkü suite her koşuşta satır ekliyor: aynı
> ölçüm 6. turda 7.669, 8. turda **7.769** çalışan gördü; denetçi 7.729 gördü.
> Bu yüzden her rakamın yanında **koşulu** yazılı.

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

> **Kart düzeltmesi (2026-08-06, M6-04 uygulaması sırasında).**
>
> **1. Şema hazır — yeni migration YOK.** `transaction_reviews` 00005'te tam
> hâliyle duruyor: `UNIQUE (transaction_id)`, bileşik same-tenant FK
> `(transaction_id, tenant_id) → transactions`, RLS beşlisi + FORCE, append-only
> trigger, `GRANT SELECT, INSERT` + `REVOKE UPDATE, DELETE`. Ölçüldü:
> `has_table_privilege('tappa_app', …)` → `transactions`,
> `transaction_reviews`, `audit_log` üçünde de `SELECT t · INSERT t · UPDATE f ·
> DELETE f`. Kartın *"bu sekme o kısıta çarpmamalı"* kriteri karşılandı — bu
> görev hiçbir yerde o iki ifadeyi yazmıyor.
>
> **2. 🔴 ORKESTRATÖRÜN *"`transaction_reviews` → 0 satır"* İDDİASI YANLIŞTI.**
> Ölçüldü (`SELECT count(*) FROM transaction_reviews`): **9 527 satır, 9 130
> tenant'ta**, hepsi `approved`, ilki 2026-08-02. Kaynağı
> `internal/db/rls_test.go`'nun izolasyon fixture'ı (`audit_log`'daki
> `iso.fixture` = 9 130 satır ile aynı sayıda tenant). **İddianın SONUCU yine de
> doğru:** üretim yolu yoktu — `action LIKE 'review%'` → **0 satır**, review HTTP
> rotası **yok**, ve `transaction_reviews`'a yazan üretim Go'su **yok**. Yani
> "audit altyapısını yeniden yazma, kullan" talimatı geçerliydi; yanlış olan
> tablonun boş olduğu cümlesiydi.
>
> **3. 🔴 KUYRUĞUN BÜYÜKLÜĞÜ TENANT BAŞINADIR — ve bu maddenin İLK HÂLİNDEKİ PAYDA
> UYDURMAYDI.** Brief `verdict='flag'` toplamını *"kuyruk"* diye adlandırıyordu;
> ben bunu düzeltirken *"64 838 tenant'a yayılı"* yazdım ve **o sayı hiçbir şeyi
> ölçmüyordu** — denetçi yeniden üretemedi, çünkü flag satırlarının dağıldığı
> tenant sayısı hiçbir zaman o kadar büyük olmadı (`transactions` append-only
> olduğu için bu sayı **yalnız büyür**, yani ölçtüğüm anda daha da küçüktü).
> **Bu, oturumun imza hatasının dokuzuncu tekrarı ve bu kez düzeltmenin İÇİNDE
> doğdu.** Aşağıdaki her sayı, onu üreten komutla birlikte yazılıdır ve
> `2026-08-06` tarihinde geliştirme veritabanında ölçülmüştür; **suite her
> koşuşta satır eklediği için hepsi artar**.
>
> ```sql
> -- Q1
> SELECT count(*) AS flag_rows, count(DISTINCT tenant_id) AS tenants_with_a_flag
>   FROM transactions WHERE verdict='flag';
>   --> flag_rows = 33 087 · tenants_with_a_flag = 14 391
> -- Q2
> SELECT tenant_id, count(*) FROM transactions WHERE verdict='flag'
>  GROUP BY tenant_id ORDER BY 2 DESC LIMIT 2;
>   --> 10000000-…-0001 = 4 778 · ikinci = 30
> -- Q3 (komşu büyüklükler — etiket bir daha kaymasın diye)
> SELECT (SELECT count(*) FROM tenants),
>        (SELECT count(DISTINCT tenant_id) FROM transactions),
>        (SELECT count(DISTINCT tenant_id) FROM employees),
>        (SELECT count(*) FROM transactions);
>   --> 100 261 · 42 569 · 68 662 · 172 873
> ```
>
> **Kararı süren rakam en büyük TEK tenant'tır** (Q2: **4 778**; ikincisi **30**),
> çünkü sayaç da liste de tenant kapsamlıdır. Bu bölümdeki performans ölçümleri
> **4 634 flag** iken alınmıştı — aynı tenant, o andaki hâli.
>
> **4. Sayaç PAHALI ve tam sayım REDDEDİLDİ.** `EXPLAIN ANALYZE`, seed tenant
> (21 419 kayıt / 4 634 flag), sıcak, **`ANALYZE` sonrası**, 3 tekrar:
> tam `count(*)` **59,4 / 63,6 / 66,4 ms** (parallel seq scan — `(tenant_id,
> verdict)` indeksi yok) · **100'de sınırlı + `ORDER BY occurred_at DESC`**
> (mevcut `transactions_tenant_occurred_idx`'i kullanır) **0,7 / 1,0 / 7,1 ms**.
> ⚠️ **Kuyruk BOŞKEN sınır hiç dolmuyor** ve tarama tenant'ın tüm zaman çizgisini
> geziyor: **17,8 / 20,3 ms** — ve bu, temiz tutulan bir panelin **kararlı
> hâlidir**. Sayaç **navigasyonda** olduğu için **her sekme görüntülemesinde**
> koşuyor; türetme `adminratelimit.go`'da tavanların yanında güncellendi.
>
> **5. 🔴 ANTI-JOIN'İN ŞEKLİ ÖLÇÜMLE SEÇİLDİ.** RLS, ilişkili `NOT EXISTS`'i
> indeks sondasına çeviremiyor (politika alt sorguyu One-Time Filter'lı bir
> `Result` düğümüne sarıyor) → planlayıcı tenant'ın tüm review indeksini
> materyalize edip her aday satır için yeniden tarıyor. Kuyruk boşken ölçüldü:
> `NOT EXISTS` (`r.tenant_id = t.tenant_id`) **1356/1375/1506 ms** ·
> `NOT EXISTS` (parametreyle) **979/1047 ms** · `LEFT JOIN … IS NULL`
> **70/100 ms** · **`NOT IN` (sevk edilen) 17,8/20,3 ms** — hashed SubPlan.
> `NOT IN` **yalnız sütun NOT NULL olduğu için** güvenli; bu, sorgunun içinde
> yazılı bir LİMİT.
>
> **6. Kabul kriteri *"Toplu onay varsa…"* — TOPLU ONAY YAPILMADI**, ve bu
> bilinçli: her karar ayrı bir hukuki eylem, ve toplu onay hem `UNIQUE
> (transaction_id)` eşzamanlılık penceresini hem audit yüzeyini çarpar. Gerekçe
> `internal/handler/review.go`'da yazılı.
>
> **7. Kriter *"Liste ve raporlar son onayı JOIN ile okur"* — LİSTE yapıldı,
> RAPORLAR M6-07.** `ListPanelTransactions`'a `LEFT JOIN transaction_reviews`
> eklendi (ölçülen maliyet: 0,745/0,622/0,463 ms → 6,084/0,632/0,611 ms; ilk
> koşu plan/önbellek artefaktı). Docket **FLAGGED kaşesini koruyor** (motorun
> kararı, §4.3) ve yanına *"Approved/Rejected by a manager"* tally'si düşüyor.
>
> **8. Yeni bir SEKME açıldı** (`/admin/review`, altıncı `PanelSection`). Gerekçe:
> günün listesi bir OKUMA, kuyruk ise günleri aşan ve işlendikçe küçülen bir İŞ
> LİSTESİ; onay butonlarını gün görünümüne koymak §4.3 sınırını markup'ta
> görünmez kılardı. **CSP kardinalitesi bozulmadı** — review sayfası **script
> yüklemiyor**, sayfalama düz link; genişletilmiş politika hâlâ **tam 1 URL'de**
> (mutasyonla doğrulandı).
>
> **9. ⚠️ ÖLÇÜLEN SIRA: TETİKLEYİCİ ÖNCE ATEŞLİYOR — ama FK GÖLGEDE DEĞİL,
> ÖLÇÜLDÜ.** §4.5 için üç savunma ayrı ayrı ölçüldü: (a) sorgunun açık
> `tenant_id` yüklemi → `pgx.ErrNoRows`; (b) **RLS** → yüklemsiz bir `SELECT` bile
> başka tenant'ın satırını **0** görüyor, kendi satırını **1** (pozitif kontrol);
> (c) **bileşik FK** → ulaşılabilir hiçbir INSERT'te ateşlenmiyor çünkü
> `tappa_check_transaction_review` BEFORE INSERT tetikleyicisi önce koşuyor ve
> **onun kendi `SELECT`'i de RLS'e tabi** olduğundan *"review target transaction …
> not found in tenant …"* (23503) veriyor. **BEN BUNU "GÖZLENEMEDİ" DİYE YAZDIM VE
> BU FAZLA TEMKİNLİYDİ:** `tappa-security-auditor` tetikleyiciyi **geri alınan bir
> transaction içinde** devre dışı bırakıp FK'yi **ateşlerken gördü**
> (`violates foreign key constraint "transaction_reviews_txn_fk"`). Yani üç
> savunmanın **üçü de** ayrı ayrı kanıtlı; suite'in göremediği tek şey (c)'nin
> **kendi koşullarında** ateşlenmesi, ve onun sebebi tetikleyicinin sırası — bir
> eksiklik değil, ölçülmüş bir sıralama.

> **Kart düzeltmesi 2 (2026-08-06, 2. tur — iki denetçi RED).** Dört bloklayan
> bulgunun dördü de **sevk edilmiş kodda gerçek kusurdu** ve dördü de aynı aileden:
> *bir cümle, sistemin vermediği/ölçmediği bir şeyi beyan ediyor.*
>
> **B1 — KARAR FORMU BÖLÜM TABLOSUNDAN OKUMUYORDU.** `review.go` *"URL bölüm
> tablosundan okunur … karar formu buraya post eder"* diyordu; `review.templ`
> `action="/admin/review"` **literalini** yazıyordu. Denetçi yalnız o literali
> `/admin/nowhere` yaptı → **tüm handler paketi YEŞİL** (gerçek HTTP + gerçek
> Postgres dahil), yani sevk edilen panelde **her Approve/Reject butonu 404**
> verirdi. Düzeltme: `ReviewView.Action` (aynı kalıp `FilterBarView.Action`) +
> `TestReviewForm_PostsToTheRouteThatIsActuallyMounted` — form hedefinin (a) bölüm
> tablosuyla aynı olduğunu **ve** (b) gerçekten mount edilmiş, gerçekten karar
> kaydeden bir adres olduğunu türetilmiş biçimde ölçüyor. Testlerin kendi
> `"/admin/review"` literalleri de `reviewHref`'e çevrildi (8 yer), yoksa ağ
> bayatlardı. **Mutasyon:** form hedefi boz → **KIRMIZI**; bölüm tablosunun
> `Href`'ini `/admin/approvals` yap → **suite YEŞİL** (form, rota ve testler
> birlikte taşınıyor — istenen davranış).
>
> **B2 — MADDE 3'ÜN PAYDASI UYDURMAYDI** (*"64 838 tenant"*). Yukarıdaki madde 3
> tamamen yeniden yazıldı: her sayının yanında **onu üreten SQL** var. Bu, oturumun
> imza hatasının **dokuzuncu** tekrarı ve bu kez **bir düzeltmenin içinde** doğdu.
>
> **B3 — 🔴 `sameOriginGate` OTURUM ÇÖZÜCÜDEN SONRA KOŞUYORDU** ve yorum *"logout
> ile aynı savunma"* diyordu. Değildi: logout kapıyı çözücüden **önce** mount ediyor
> ve `adminlogin.go` bunu *"a FREE refusal, BEFORE the resolver"* diye açık bir
> tasarım kuralı olarak yazıyor. Ölçülen zarar: cross-origin POST başına **1 oturum
> çözücü okuması**, ve **300 istek müdürün KENDİ 300'lük oturum bütçesini
> tüketiyordu** (`SameSite=Lax` gerçek cross-*site*'ı keser ama **same-site/farklı
> origin**'i — alt alan, http ikizi — kesmez). Düzeltme: `AdminAuth.ProtectWriting`
> (`floodGate → sameOriginGate → requireAdmin → sessionGate`), POST `Protect`
> grubunun **dışına** taşındı. **Ağ:** `fakeAdmins`'e çözücü sayacı eklendi;
> `TestReviewDecision_ACrossOriginRefusalCostsNoDatabaseWork` 300 cross-origin
> POST'tan sonra **çözücü çağrısı = 0** ve müdürün kendi `GET`'i **200** olmasını
> istiyor. **Mutasyon:** eski sıra → **KIRMIZI** (sayaç kolu ayrıca kısaltılmış
> döngüyle de doğrulandı: *"5 session lookup(s), want 0"*).
> ⚠️ Bu, M6-01 B'deki *"kopyaladığın kalıbın parçalarını say"* dersinin aynısı.
>
> **B4 — EKRAN ÖLÇMEDİĞİ BİR ÖZNE SÖYLÜYORDU (§4.6).** Çift tıklayan müdüre
> *"somebody else decided that record first"* deniyordu; **kararı kendisi vermişti**.
> Düzeltme: `GetTransactionReview` (açık `tenant_id` yüklemi, kuşak ağı kapsamında),
> `review.DecidedError{ByYou, Outcome}` (`errors.Is(…, ErrAlreadyDecided)` hâlâ
> doğru), ve *"You already decided this one — you had already recorded it as
> approved/rejected"*. Okuma **ikinci bir transaction'da** koşar (ilk transaction
> 23505 ile abort olmuştur). **Mutasyonlar:** okumayı sil → **KIRMIZI**; kayıtlı
> yerine **denenen** outcome'u raporla → **KIRMIZI**. Eşzamanlılık testi de artık
> **her yarışçıya ayrı reviewer id** veriyor ve hiçbir kaybedene *"sen karar
> verdin"* denmediğini ölçüyor.
>
> **Bloklamayan beşi de kapatıldı:** notun *"templ kaçırır"* gerekçesi **yanlıştı**
> (ölçüldü: `transaction_reviews.note` **hiçbir sorguda SELECT edilmiyor**, bugün
> write-only; ilk tüketici muhtemelen M6-07 CSV'si ve **templ kaçışı almayacak** →
> `= + - @` ile başlayan hücre formül olur) · marka sınıflandırmasının **gerekçe**
> cümleleri bayatlamıştı (`tomato` artık `.stamp` içinde değil, `.tally--rejected`
> de kullanıyor) · `sameOriginGate` log satırı **her reddi sign-out sanıyordu**
> (artık method+path yazıyor, ağı var, mutasyon **KIRMIZI**) · POST'un **gövde
> sınırı yoktu** (net/http'nin 10 MB'ı; artık `maxReviewBody = 8 KiB`, kalıp
> `checkin.go`'dan, ağ + mutasyon **KIRMIZI**) · `adminauth/cookie.go`'nun
> `SameSite=Lax` gerekçesi **var olmayan bir senkronizasyon token'ına** dayanıyordu.
>
> **Notu bugün göstermenin maliyeti ÖLÇÜLDÜ, karar VERİLMEDİ** (kullanıcı kararı
> bekliyor) — ayrıntı için `internal/handler/review.go`'daki `reviewNote` bloğu:
> sorgu maliyeti **ayırt edilemez** (`rv.outcome` 0,378–2,156 ms · `rv.outcome` +
> `rv.note` 0,308–0,406 ms, aynı gün/aynı tenant, 3'er tekrar), bugün **9 940
> review satırının 20'sinde** not var (en uzunu **24 karakter**), en kötü sayfa
> etkisi **25 × 500 = 12,5 KB** (~32 KB'lık sayfada **+%39**). Yani seçenek (a)
> *bugün göster* teknik olarak ucuz; seçenek (b) *write-only bırak* daha az yüzey.

> **Kart düzeltmesi 3 (2026-08-06, 3. tur — KULLANICI KARARI).**
>
> **KARAR: `transaction_reviews.note` ARTIK RENDER EDİLİYOR.** 2. turda iki seçenek
> ölçülüp önüne konmuştu; kullanıcı **(a) bugün göster**'i seçti. Kararı süren
> sayılar, tekrar bulunabilsin diye burada:
>
> | Ölçüm | Değer (**2026-08-06**, karar anı) | Üreten |
> |---|---|---|
> | not taşıyan review satırı | **9 940'ta 20** | `SELECT count(*), count(note), max(length(note)) FROM transaction_reviews` |
> | en uzun not | **24 karakter** | aynı sorgu |
> | sorgu maliyeti farkı | **ayırt edilemez** — `rv.outcome` 0,378–2,156 ms · `+rv.note` 0,308–0,406 ms | `EXPLAIN (ANALYZE)`, aynı tenant/gün, 3'er tekrar |
>
> ⚠️ **İLK İKİ SATIR BİR ANIN FOTOĞRAFIDIR VE ARTIK GEÇERSİZDİR — ONLARI GEÇERSİZ
> KILAN ŞEY BU GÖREVİN KENDİ TESTİ.** `TestReviewDB_ALongNoteIsCutAndTheManagerIsTold`
> her koşuda **501 karakterlik** bir not yazıyor, yani *"dev DB'deki en uzun not"*
> tanımı gereği `maxReviewNote` oldu. Aynı sorgu, düzeltme turunda: **10 189 review ·
> 55 not · en uzun 500**. Sayılar **kararın alındığı andaki** hâli olarak duruyor
> (kararı onlar sürdü); **bir daha suite'in kirlettiği bir büyüklüğe yaslanma** —
> ya sabiti (`maxReviewNote`) referans al ya da ölçümün tarihini yaz. Bu, sayı-etiketi
> sınıfının **onuncu** vakası ve bu kez hatayı **kendi düzeltmem** üretti.
>
> ⚠️ **Ama kararın GERÇEK gerekçesi maliyet değil:** write-only bir alan, ürünün
> **kullanıcının yazdığını sessizce kestiği** anlamına geliyordu. 500 karakterlik
> sınır aşıldığında müdür *"Recorded as approved"* görüyor ve **kaybettiğini hiçbir
> yerde öğrenemiyordu**.
>
> **NE YAPILDI.** `rv.note` → `ListPanelTransactions` → `ledger.Record.ReviewNote` →
> `components.DocketView.ReviewNote` → docket'ta **kararın yanında** (*"Manager's
> note"*). Kuyruktaki kayıtlar tanım gereği kararsızdır, yani not yalnız
> **Transactions**'ta görünür — **karar veren ile okuyan aynı yüzeyi paylaşır**.
> Kırpma artık **görünür**: `reviewNote` `(note, clipped)` döndürüyor, başarı
> yönlendirmesi `&clipped=1` taşıyor, onay kutusu *"longer than 500 characters, so
> only the first 500 were saved"* diyor — **ve** kalan metin docket'ta okunabiliyor.
>
> **SAYFA BOYU ÖLÇÜLDÜ** (gerçek handler, 25 docket, hepsi karara bağlı):
> **31 013 B** (notsuz) · **33 663 B** (+%8,5, dev DB'deki en uzun not) ·
> **45 563 B** (+%46,9, hepsi 500 karakter). ⚠️ **Tahminim 12,5 KB / %39'du, gerçek
> 14 550 B / %46,9** — çünkü her not etiketini ve markup'ını da taşıyor. Fark
> M6-03'ün roster'ından ayrı: bu **sınırlı** (işletme büyüklüğüyle değil, yalnız
> müdürün yazdığıyla büyür, ve sabit onu bağlar).
>
> 🔴 **KAÇIŞ İDDİA EDİLMEDİ, KANITLANDI.** Bu paragraf **üçüncü kez** yazıldı ve ilk
> ikisi başka bir sistemi tarif ediyordu (önce var olmayan bir renderer, sonra
> write-only bir alan — ikincisi **bir gün içinde** eskidi).
> `TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered` gerçek POST ile
> `Maria & "the boss" said <script>alert(1)</script> — ħadd ma kien hemm` saklıyor,
> DB'de **verbatim** olduğunu, docket'ta **`&lt;script&gt;`** olarak çıktığını,
> **çift kaçış olmadığını** (`&amp;amp;` yok) ve sayfadaki `<script` sayısının
> **tam 1** (vendor htmx) kaldığını ölçüyor. ⚠️ Testin **ilk hâli tüm sayfada**
> `</script>` arıyordu ve **htmx etiketine takıldı** — kapsam karta değil karta
> ait: ölçüm birimi **notun kendi kartı**.
>
> **M6-07 CSV UYARISI DURUYOR:** templ kaçışı CSV'ye geçmez; `= + - @` ile başlayan
> hücre Excel/Sheets'te formüldür ve önekleme **o görevin işidir**. Notun render
> ediliyor olması bunu kapatmaz.
>
> **Mutasyonlar (hepsi aynı turda):** sorgudan `rv.note` düş → **KIRMIZI** ·
> docket'tan render'ı sil → **KIRMIZI** (iki test) · `clipped` sinyalini sil →
> **KIRMIZI** · yeni satırın tonunu `ink/30` yap → `TestBrand_EveryInkToneClearsAA`
> **KIRMIZI** (1,86:1) · `ListPanelTransactions`'ın `tenant_id` yüklemini sil →
> kuşak ağı **KIRMIZI**. **Ve mapper-eşlik ağı GÜÇLENDİRİLDİ:** `rv.note` sorgudan
> düşünce ağ **yeşil kalıyordu** (muafiyet iki tarafı birden mazur görüyordu);
> artık muaf alan için **gün mapper'ının doldurduğu** da isteniyor → aynı mutasyon
> **KIRMIZI**.

> **Kart düzeltmesi 4 (2026-08-06, 4. tur — denetçi: *"No behavioural defect was
> found in this round"*).** Bütün bulgular **yorum katmanındaydı**, ve dördü de bu
> görevin imza sınıfından: *bir cümle, sistemin vermediği bir şeyi beyan ediyor.*
>
> **F1 — 🔴 `layout.Panel`'İN ÜRÜN GENELİNDE SIFIR ÇAĞIRANI VAR**, ama üç yorum
> bölümlerin onu render ettiğini söylüyordu (biri **bu görevde yazılan** yeni dosya,
> ikisi **bu görevde düzenlenen** satırlar — yani hatayı ben taze yazdım). Ölçüm:
> `rg -n '@layout\.Panel\b' --glob '*.templ'` → **hiç eşleşme yok**;
> `rg -c 'layout\.Panel\(' --glob '*_templ.go'` → **0**. Her bölüm koşulsuz
> `pages.PanelShell` → `layout.PanelWithScript` yolundan gidiyor. **Neden kusur:**
> `base.templ` `Panel` için *"bu kabuğa script yuvası vermek, o politikayı
> genişletmeyi başka bir yerde tek kelimelik bir düzenleme yapar"* diyor — ve review
> bölümünde **o güvence yok**; denetçi tam o tek kelimelik düzenlemeyi yaptı,
> **derlendi ve render edildi**, yalnız bir TEST itiraz etti. Dört yorum (üçü +
> `Panel`'in kendi bloğu) **ölçüldüğüne eşitlendi ve doğrulayan komut yorumun içine
> yazıldı**.
>
> **F1-b — İKİ OKUMA ÖLÇÜLDÜ, KARAR KULLANICININ.** İkisi de gerçekten uygulanıp
> ölçüldü ve geri alındı (ağaç `ba41b253…`'e döndü):
>
> | | **(a) güvenceyi gerçek yap** | **(b) ölü bileşeni sil** |
> |---|---|---|
> | değişen el yazısı satır | **+49 / −29**, 3 `.templ` dosyası (`PanelShell` ikiye ayrılır + ortak `panelChrome` çıkarılır) | **−20 satır** (`base.templ`'deki blok), 1 dosya |
> | test değişikliği | **0** — mevcut suite değişmeden geçiyor (`ok internal/handler 32.213s`) | **0** — hiçbir şey referans vermiyor (`ok … 33.929s`) |
> | CSP kardinalite pini | etkilenmiyor, ısırmaya devam ediyor | etkilenmiyor |
> | **denetçinin tek kelimelik düzenlemesi** | 🔴 **DERLENMİYOR** — `too many arguments in call to PanelShell / have (PanelChrome, string) / want (PanelChrome)` | derlenir (bugünkü hâl) |
> | genişletmenin kalan yolu | `@PanelShellWithScript(...)` yazmak — **derlenir**, ve test **KIRMIZI** verir | dize değiştirmek — derlenir, test **KIRMIZI** verir |
> | tek savunma silinirse | `PanelShellWithScript` yine adlandırılmak zorunda (ayrı bileşen) | **hiçbir şey kalmaz**: testi etkisizleştirip tek kelimelik düzenlemeyi uyguladım → **derlendi ve tüm paket YEŞİL** |
>
> **ÖNERİM (a)** — ama karar kullanıcınındır. Gerekçe: (a) `Page`/`PageWithScript`
> deseninin aynısını panelde de kurar, mevcut hiçbir ağı bozmaz, ve **string
> düzenlemesiyle genişletmeyi derleyici seviyesinde imkânsız kılar**. ⚠️ **Fazla
> iddia etmiyorum:** (a) genişletmeyi **imkânsız kılmıyor**, yalnız *"başka bir
> bileşen adlandır"* hâline getiriyor — yani ölçülen kazanç **bir dize düzenlemesi
> ile bir tanımlayıcı düzenlemesi** arasındaki fark, artı derleyicinin ilkini
> reddetmesi. (b) ucuz ve dürüst ama tek savunmayı bir teste bırakır ve **o testi
> silen mutasyon her şeyi serbest bırakıyor** (ölçüldü).
>
> **F2 — `reviewDecision`'ın başlığı *"IT ANSWERS 303, ALWAYS"* diyordu**, oysa aynı
> fonksiyonun 75 satır aşağıdaki `default:` dalı **500 render ediyor** (ve doğru
> yapıyor — bir kesinti reddedilmiş bir karar gibi gösterilemez). Cümle *"çağıranın
> girdisinin seçebildiği her dal"* olarak daraltıldı.
>
> **F3 — `problem=gone` iki erişilebilir dalda ölçülmemiş bir olguyu beyan
> ediyordu** (§4.6, B4'ün daha hafif ikizi). Aşırı gövde ve ayrıştırılamayan id
> *"that record is not waiting for a decision any more"* gösteriyordu; **aşırı
> gövdede kayıt HÂLÂ bekliyor**, ayrıştırılamayan id'de **hiç kayıt yok**. Yeni
> dördüncü kelime **`unreadable`**: *"We could not read that submission … The record
> is still in the queue."* ⚠️ **Varlık kehaneti KAPALI KALDI** — üç gerçek KAYIT
> sorusu (yok / başkasının / flag değil) hâlâ tek cevaba çöküyor ve bunun kendi
> testi var.
>
> **F4 — bir parantez yazıldığı anda bayattı ve onu BENİM KENDİ TESTİM bayatlattı**
> (bkz. yukarıdaki tabloda ⚠️).
>
> **F5 — `TestReviewDB_OnlyFlaggedRecordsAreReviewable` HTTP üzerinden yalnız `ok`'u
> sürüyordu**; artık **üçü de** (`ok`/`reject`/`ignored`) tablo bazlı, hem panel hem
> tetikleyici kolunda. Fixture yoksa satırı kendisi yazıyor.

> **Kart düzeltmesi 5 (2026-08-06, 5. tur — KULLANICI KARARI: F1-b → seçenek (a)).**
>
> **KARAR: script'siz bölümler artık gerçekten `layout.Panel` render ediyor.**
> `pages.PanelShell` ikiye ayrıldı — `PanelShell(c)` (script yuvası **yok**) ve
> `PanelShellWithScript(c, script)` — ortak markup `panelChrome`'a çıkarıldı.
> Bugün: `@layout.Panel(` **1 çağıran**, `@PanelShellWithScript(` **1 çağıran**
> (Transactions), `@PanelShell(` **2 çağıran** (Review + dört boş bölümün ortak
> şablonu). Üreten komut: `grep -rn --include='*.templ' '@PanelShell' web/templates`.
>
> **KARARI SÜREN TEK ÖLÇÜM** (4. turda alındı, ikisi de gerçekten uygulanıp geri
> alındı):
>
> | | **(a) seçilen** | (b) ölü bileşeni sil |
> |---|---|---|
> | el yazısı satır | +49 / −29, 3 `.templ` | −20, 1 dosya |
> | test değişikliği | **0** | **0** |
> | denetçinin tek kelimelik düzenlemesi | 🔴 **derlenmiyor** | derlenir |
> | **tek savunma (CSP testi) nötralize edilirse** | `PanelShellWithScript` yine adlandırılmak zorunda | 🔴 **hiçbir şey kalmıyor** — ölçüldü: derlendi, **paket yeşil** |
>
> **ÖLÇÜM TESTE ÇEVRİLDİ** — 4. turda o kanıt yalnız benim elimdeydi, repoda değildi.
> İki yeni ağ, **ikisi de türetilmiş** (sabit liste değil, M5-10 dersi):
> `TestPanelShells_TheScriptlessShellHasNoScriptSlot` sevk edilen **fonksiyon
> tiplerini** reflection ile okuyor (script'siz kabuk **hiç `string` almamalı**;
> yuva **tam olarak** ötekinde olmalı) · `TestLayoutShells_EveryOneIsActuallyRendered`
> `layout/base.templ`'den **kabukları**, üründen **çağıranları** türetip her
> exported kabuğun gerçekten render edildiğini istiyor — **F1'in kendi kusur sınıfı**
> (yarın eklenen bir kabuk kendiliğinden kapsama girer).
>
> **Mutasyonlar (hepsi aynı turda):** **M-AA** tek kelimelik düzenleme →
> **DERLENMİYOR** (`too many arguments in call to PanelShell / have (PanelChrome,
> string) / want (PanelChrome)`) · **M-AB** `PanelShellWithScript` adlandır →
> **derleniyor**, CSP testi **KIRMIZI** (kalan yol **açıkça yazılı**) · **M-AC**
> yuvayı geri koy → yeni invaryant **KIRMIZI** · **M-AD** `layout.Panel`'i yeniden
> öldür → ölü-kabuk ağı **KIRMIZI** · **M-AE** **yedinci** bölüm ekle → türetilmiş
> ağlar onu **kendiliğinden** görüyor (rota + kapı + politika) · **M-AF** o yedinci
> bölüm de script yüklesin → kardinalite pini **KIRMIZI** (yani pin **6'da değil,
> N'de** ısırıyor).
>
> ⚠️ **AŞIRI İDDİA YOK, VE BU KASITLI.** (a) genişletmeyi **imkânsız kılmıyor**:
> `@PanelShellWithScript` yazmak hâlâ derleniyor ve onu durduran şey testtir. Kazanç
> dar ve gerçek — **en ucuz hamle (bir dizeyi düzenlemek) artık derleyici hatası**.
> Yorumlara *"imkânsız/tamamen"* yazılmadı; kalan yol adıyla yazıldı ve nasıl
> denendiği (M-AB) kayıtlı.
>
> **BEŞİNCİ YORUM TURU.** (a) uygulanınca 4. turda düzelttiğim cümlelerin bir kısmı
> yine yanlış oldu (*"layout.Panel'in çağıranı yok"* artık **yanlış**). Beş cümle
> yeniden yazıldı ve **yazmadan önce grep'lendi**; doğrulayan komut yorumun içinde.
> Bu görevde yorum katmanı **beş kez** düzeltildi — sınıfın kendisi budur: *bir
> cümle, sistemin verdiğinden farklı bir şey beyan ediyor.*

> **Kart düzeltmesi 6 (2026-08-06, 6. tur — KAPANIŞ).** `tappa-security-auditor`
> **ONAY**; genel göz yalnız metin katmanında RED — **ikinci ard arda yalnız-metin
> turu**, yani mekanizma oturdu.
>
> **T1 🔴 — 5. TURUN SİLDİĞİ YOLU ANLATAN BİR BAŞLIK SEVK EDİLMİŞTİ.**
> `review.templ` *"NO SCRIPT — AND THAT IS A RUNTIME ARGUMENT, NOT A STRUCTURAL
> GUARANTEE … this section passes `""` to pages.PanelShell, which passes it on to
> layout.PanelWithScript"* diyordu — **dört yan cümlenin dördü de yanlış** ve başlık
> tam olarak kullanıcının bir tur önce **satın aldığı güvenceyi inkâr ediyor**.
> Gerçek: `@PanelShell(v.PanelChrome)`, tek argüman, ve `PanelShell` `layout.Panel`
> render ediyor. ⚠️ **4. turdaki F1'in tersi, bir tur sonra, 5. turun dokunduğu
> dosyada** — ve *"yazmadan önce grep'ledim"* iddiasını yanlışlıyor. Bu **altıncı
> yorum turu**; bu kez **yazdıktan sonra da** grep'lendi ve çıktı raporda.
>
> **T2** — `review.go`'nun düzyazısı, **dört satır yukarıdaki kendi uyarısını**
> çiğneyerek suite'in kirlettiği bir büyüklüğü aktarıyordu (*"20 notes across
> 9 940 reviews"*; bugün 10 295/64/500). Sayı **kaldırıldı**, yerine neden
> alıntılanamayacağı yazıldı.
>
> **T3** — `base.templ` *"NOTHING PASSES `""` TO THIS COMPONENT ANY MORE"* diyordu;
> `layout.Panel` **altı satır yukarıda** tam olarak onu yapıyor. ⚠️ **Daha kötüsü:
> yorumun içine koyduğum doğrulama grep'i (`@layout.PanelWithScript(`) kardeş
> çağrıyı YAPISAL OLARAK göremez** (kardeş paket öneki kullanmaz) — yani **yanlış
> cümleyi doğrulayan bir komut** bırakmışım. Grep de düzeltildi
> (`@(layout\.)?PanelWithScript\(`) ve iki çağıranı da gösteriyor.
>
> **T4** — `TestLayoutShells_EveryOneIsActuallyRendered` bir **metin taramasıydı**:
> `.templ` yorumları bu repoda taşıyıcı metindir, yani bir **yorum** ölü bir kabuğu
> "canlı" tutabilirdi. **Düzeltildi** (yorum satırları taranmadan önce atılıyor) +
> `TestStripTemplComments_…` kontrolü. **Mutasyon M-AG:** gerçek çağıranı öldür,
> onu adlandıran bir yorum bırak → **KIRMIZI**. `Wordmark`'ın "kabuk" sayılması
> **limit olarak yazıldı** (zararsız aşırı kapsam; muafiyet listesi bu testin
> kaçındığı sabit liste olurdu).
>
> **T5 🔴 (§4.6) — ONAY BANDOSU SORGU DİZESİNDEN BASILIYORDU.**
> `?done=approved&clipped=1` → *"Recorded as approved"* + kırpma cümlesi, **DB'de 0
> review satırı**, ve bağlantı olarak gönderilebiliyor. **Seçilen çare: bandoyu
> ÖLÇÜME çevirmek** — redirect artık `&for=<txn>` taşıyor ve sayfa `ledger.Decision`
> ile **doğruluyor**. **Neden bu, imzalı flash değil:** kusur bir **yanlış BEYAN**,
> sahte bir **yetki** değil — burada yetkilendirilecek bir şey yok; ve bu çare **yeni
> SQL istemiyor** (`GetTransactionReview` zaten vardı), yalnız `?done` taşıyan
> GET'lerde **bir indeksli okuma** ekliyor. İmzalı flash yeni bir sır taşıyan değer,
> yeni bir son kullanma ve yeni bir hata yüzeyi demekti. ⚠️ **Ne kanıtlamıyor,
> yazılı:** kararı **bu operatörün** ya da **az önce** verdiğini değil — yalnız
> kararın **var olduğunu**. **Mutasyon M-AJ:** dizeyi yine inan → **KIRMIZI** (dört
> vaka).
>
> **T6 🔴 (§4.7) — `ParseForm` hatası ham basılıyordu** ve `net/url` **hatalı girdiyi
> alıntılıyor**: bozuk yüzde-kaçışlı gövdede log satırına müdürün notundan ~3 bayt
> düşüyordu — bu dosyanın kendi kuralı *"THE NOTE IS NOT LOGGED"* iken. Artık
> **sınıflandırılmış sebep** basılıyor (`malformed form` / `body over the limit`),
> ham hata hiç basılmıyor. **Kanarya testi kalıcı** + pozitif kontrol (reddin
> **loglanmaya devam ettiği**). **Mutasyon M-AI:** `"err", err` geri koy →
> **KIRMIZI** (`the log carries "%ZZ"`).
>
> **T7 — ADR yazıldı: [`docs/adr/0009-onay-karari-geri-alinamaz.md`](../adr/0009-onay-karari-geri-alinamaz.md).**
> Verilmiş bir karar **hiçbir yoldan** düzeltilemiyor (ölçüldü, `tappa_owner`
> superuser olarak: `UPDATE`/`DELETE` **ikisi de trigger'la reddedildi**), yani
> §4.3'ün kendi telafi yolu (*"yeni kayıt + audit_log"*) bu tabloda **yapısal olarak
> kullanılamaz** — ve bu **bilinçli** (00005 gerekçeli). **§4.3 ihlali değil**
> (güvenlik denetçisi); kayda değer olan **hiçbir dosyanın söylememesiydi**. Bugünkü
> etki **sunumla sınırlı** (`transaction_reviews`'ü okuyan **rapor sorgusu yok**,
> grep'le doğrulandı); **M6-07'de parasal olur** ve ADR üç düzeltme yolunu, her
> birinin neyi feda ettiğiyle birlikte yazıyor.
>
> **🔴 VE BU TUR BİR AĞIN KENDİSİNDE DELİK BULDU** (T5'in mutasyonu ortaya çıkardı):
> kuşak ağının türetimi `\bq\.([A-Z]\w+)\(` arıyordu — yani **alıcının `q` ADINDA
> olmasını** şart koşuyordu. `GetTransactionReview` **iki pakette de**
> `store.New(tx).GetTransactionReview(...)` diye çağrılıyor, yani sorgu **hiçbir ağ
> tarafından görülmüyordu**: `(r.tenant_id = @tenant_id OR TRUE)` mutasyonu **iki
> testi de yeşil bıraktı**. Türetim artık **herhangi bir** büyük harfli metot
> çağrısını tarayıp `db/queries`'te **gerçekten tanımlı** olanlarla kesişiyor —
> bir değişken adı seçerek atlatılamaz. **Mutasyon M-AL:** aynı `OR TRUE` →
> **her iki pakette de KIRMIZI**.

> **Kart düzeltmesi 7 (2026-08-06, 7. tur — İKİ AĞ DA YENİLDİ, ikisi de yapıyla
> değiştirildi).** Bu turun bulguları **metin değil, gerçek**: kendi yazdığım iki ağ
> ölçülerek kırıldı.
>
> **B1 🔴 — `stripTemplComments` TEK BİR HTML YORUMUYLA YENİLDİ.** Denetçi
> `layout.Panel`'in gerçek çağrısını öldürüp yerine 5. turun kaldırdığı şekli koydu ve
> tek satır bıraktı: `<!-- historical note: this used to be @layout.Panel( -->` →
> `make templ` OK · `go build` OK · **16/16 `ok`**. Yani bileşen ölüyken **ve
> kullanıcının 2026-08-06 kararı geri alınmışken** hiçbir yerde kırmızı yok.
> ⚠️ **Bunu "kabul edilmiş limit" sayamam:** yazılı limitim iki şekli *"kapattığı
> delikten daha dar"* diye nitelemişti; `<!-- -->` **aynı deliktir**, daha darı değil.
> **Çare yapısal:** ağ artık **üretilen Go'yu `go/ast` ile** okuyor — templ hiçbir
> yorum biçimini üretilen Go'ya taşımıyor, ve bir dize literali de çağrı değil.
> Tazelik ayrıca kontrol ediliyor (her `.templ`'in bildirdiği her bileşenin
> kardeş `_templ.go`'da bir `func`'u olmalı) ve **gövde tazeliğinin bu testin işi
> olmadığı** açıkça yazıldı (onu `make gen` + `git diff --exit-code` tutar).
>
> **B2 🔴 — KUŞAK AĞI SATIR SONUNDAKİ NOKTAYLA ATLATILDI, VE `gofmt` BİRLEŞTİRMİYOR.**
> `\.([A-Z]\w+)\(` regex'inin üstünde *"finds **ANY** method call"* yazıyordu; denetçi
> 8 şekil denedi, **3'ü kaçtı**. Uçtan uca: `ListFlaggedForReview`'un
> `WHERE t.tenant_id = @tenant_id` yüklemi **tamamen silindi**, çağrı `q.`⏎`Method(`
> yapıldı → `gofmt -l` **boş**, `go vet` OK, `go build` OK, **iki belt de yeşil**.
> **Ben de yeniden ürettim** (aynı sonuç). **Çare yapısal:** türetim artık
> `go/parser` + `go/ast` ile **`*ast.SelectorExpr`** geziyor — AST boşluk görmez, ve
> **selector'ları** (yalnız çağrıları değil) taradığı için metot-değeri şeklini de
> yakalıyor.
>
> **ÜÇ ATLATMA ŞEKLİ, TEK TEK, DÜZELTMEDEN SONRA** (yüklem silinmiş hâlde):
>
> | Şekil | `gofmt -l` | `go build` | Belt |
> |---|---|---|---|
> | `q.`⏎`ListFlaggedForReview(…)` | boş | OK | 🔴 **KIRMIZI** |
> | `q.ListFlaggedForReview (…)` (boşluk) | — | OK | 🔴 **KIRMIZI** |
> | `call := q.ListFlaggedForReview` sonra `call(…)` | — | OK | 🔴 **KIRMIZI** |
>
> **B1 için de üç şekil:** denetçinin `<!-- … -->` yorumu → **KIRMIZI** · Go tarzı
> `//` yorum → **KIRMIZI** · kabuk adı **dize literalinde** → **KIRMIZI**.
>
> ⚠️ **KAPATAMADIĞIM — LİMİT.** `reflect.ValueOf(q).MethodByName("X")`: sorgu adı bir
> **dize**, selector değil, yani AST göremez. ~~**Derlenen bir örnek kuramadım**
> (denedim, derlemedi), o yüzden bu kaçış **ölçülmedi, savunuldu**.~~
> 🔴 **BU CÜMLE 8. TURDA ÇÜRÜTÜLDÜ — kaçış DERLENİYOR ve ÖLÇÜLDÜ** (aşağıdaki
> *Kart düzeltmesi 8* → B2). Benim ilk denemem `err` yeniden bildirimiyle patlamıştı;
> hata kaçışta değil, denememdeydi. İkinci limit — **başka bir pakette** yapılan
> çağrı bu paketin AST'sinde yok — geçerliliğini koruyor.
>
> **🔴 İKİ CÜMLE GERİ ÇEKİLDİ.** *"ANY method call"* ve *"kapattığı delikten daha
> dar"* — ikisi de ölçümle yanlışlandı ve ikisi de ölçtüğüne eşitlendi. Yeni
> cümleler yazılmadan önce **yine yenilmeye çalışıldı** (yukarıdaki altı şekil).
>
> **N1 — AĞIN KAPSAMI YAZILDI: 41 sorgunun 8'i (%20).** Üreten komut:
> `grep -rhoE '^-- name: [A-Za-z_]+' db/queries/*.sql | wc -l` → **41**; iki domain
> paketinin AST'sinden türetilen küme **8**. Kalan **33** sorgu için (§4.5'in ikinci
> savunması) **hiçbir ağ yok** — RLS satırları yine durduruyor, korumasız olan
> **açık yüklem**. Genişletmenin ölçülüp elendiği gerekçe (`LockEmployeeForTap`'ın
> `WHERE`'i yok) da yanına yazıldı.
>
> **N2 · N3 — iki bayat düzyazı** (*"five tabs of navigation"*, *"the five sections
> are there"*) ölçüme eşitlendi; ikisinin de altındaki kod zaten `PanelSections`'tan
> türetilmişti.
>
> **N4 — C2'nin okuması bütçe aritmetiğine girdi.** `?done` doğrulaması
> `adminratelimit.go`'da tavanların yanında sayıldı: **0,367 / 0,081 / 0,095 ms**
> (denetçi) ve **0,427 / 0,190 / 0,106 ms** (burada yeniden üretildi, id'yi bir alt
> sorgu çözdüğü için biraz yüksek), **ikisi de aynı planda**
> (`Index Scan using transaction_reviews_tenant_idx`). Panel isteğinin ~4–27 ms'ine
> karşı düşük uçta **~%0,4**; yalnız `?done` taşıyan GET'te, yani **karar başına bir
> kez**. `review.go`'daki *"ONE indexed lookup"* artık sayı taşıyor.

> **Kart düzeltmesi 8 (2026-08-06, 8. tur — B1 KAPATILDI; bundan sonra KAPATMA YOK,
> SAYMA VAR).** Kapanış kuralı (M5-06 md. 11) bu turdan itibaren geçerli: son üç tur
> aynı desendeydi — her ağ bir sonraki turda yeniliyor.
>
> **B1 🔴 KAPATILDI — kuşak ağının OKUYUCUSU başka bir dosyadaki YORUMA çözülüyordu.**
> `queryBody` sorguyu **satır başına sabitlenmemiş** `strings.Index(raw, "-- name: X ")`
> ile arıyordu; `declaredQueries` ise **satır-sabitli** `(?m)^-- name: (\w+) `
> kullanıyordu — **tek bir olgu için iki desen, ve gevşek olan OKUYUCUYA aitti.**
> Denetçinin üç adımı **birebir yeniden üretildi:** ① `ListFlaggedForReview`'un
> `WHERE t.tenant_id = @tenant_id`'i **tamamen silindi** ② `admins.sql`'e (alfabetik
> olarak **önce**, `os.ReadDir` sırası) **tek yorum satırı** ③ çağrı yeri **el
> değmemiş** → `make sqlc` **sessizce başarılı**, `internal/store` **kapsamsız
> sorguyla** yeniden üretildi, **`make test` → 16/16 `ok`**. **Pozitif kontrol:**
> yalnız yorum silinince (yüklem hâlâ silik) → **FAIL**, yani yorum tek sebep.
> **Çare:** okuyucu artık **türetimle aynı** satır-sabitli deseni kullanıyor
> (`queryMarker`), ve sonraki başlık araması da sabitli. `TestQueryBody_IsBounded`
> (iki pakette de) artık **işaretçi kaçırmayı** da kapsıyor + çapa için pozitif
> kontrol. **Aynı üç adım, düzeltmeden sonra → KIRMIZI.**
>
> **B2 🔴 KARTIM YANLIŞTI — yansıma kaçışı DERLENİYOR.** 7. turda *"derlenen bir örnek
> kuramadım … ölçülmedi, savunuldu"* yazmıştım. Denetçi kurdu, ben de **yeniden
> ürettim** (ilk denemem `err` yeniden bildirimiyle patladı; denetçinin `err = nil`
> biçimi doğru): `reflect.ValueOf(q).MethodByName("List" + "FlaggedForReview")` →
> `gofmt -l` **boş** · `go vet` **OK** · `go build` **OK** · yüklem **silikken iki
> belt de `ok`** · `TestReviewDB_TheQueueCountIsTheQueue` **`ok`** (kuyruk çalışmaya
> devam ediyor). Yani bu **ölçülmüş bir delik**. **KAPATILMADI — SAYILDI**
> (kapanış kuralı): kapatmak bu pakette yansımayı yasaklamak ya da çağrı grafiğini
> tip-denetlemek demek; ikisi de görev, satır değil. Koddaki *"Neither is closed"*
> cümlesi ölçüme eşitlendi.
>
> **B3 🔴 TAZELİK KAPISI VAR OLMAYAN BİR ÇAREYE HAVALE EDİYORDU.** *"…which `make
> check` runs"* yazmıştım. **`check: fmt lint test`** ve `fmt` = `gofmt -w` +
> `templ fmt` — **biçimlendirici**; `gen`/`templ generate`/`sqlc` `check`'te **hiç
> geçmiyor**, CI de yalnız `make check` + `make audit` koşuyor. **Uçtan uca
> ölçüldü:** `admin.templ` değişti (`grep -c '@layout.Panel(' → 0`), `make gen`
> **koşulmadı**, `make fmt` koştu → `admin_templ.go` **değişmedi** (hâlâ
> `layout.Panel(` **1**), ve üç panel testi de **`ok`**. Cümle **geri çekildi**;
> boşluk **sayıldı**. ⚠️ **CI dosyasının kendi yorumu da aynı yanlışı yapıyor**
> (`ci.yml:88-90` *"`make gen`… çıktısının commit edildiğini de doğrular"*) — kapsam
> dışı, kayda geçti.
>
> **B3 — İKİ OKUMA, SAYILARLA (karar KULLANICININ; `Makefile` DEĞİŞTİRİLMEDİ):**
>
> | | **(a) `check`'e `gen` ekle** | **(b) limit olarak yaz + backlog** |
> |---|---|---|
> | `make gen` süresi | **3,34 sn** (`/usr/bin/time -p make gen`) | 0 |
> | `check`'in bugünkü süresi | `fmt` **1,13** + `lint` **2,82** + `test` **~110–140 sn** ≈ **115–145 sn** | aynı |
> | ek maliyet | **+3,34 sn ≈ %2,3–2,9** | yok |
> | `gen` **deterministik mi** | **EVET** — iki ardışık koşu, üretilen dosyaların birleşik shasum'ı **birebir aynı** (`da7df7fb…`) → `git diff --exit-code` yanlış kırmızı vermez | — |
> | CI'da `templ`/`sqlc` var mı | **evet**, `go run …@sürüm` ile pinli; CI zaten Go 1.26.5 + modül indiriyor | — |
> | bir şey kırıyor mu | temiz ağaçta `make gen` sonrası **ek diff yok** | — |
> | kapattığı şey | bayat `_templ.go`/`*.sql.go` **CI'da kırmızı olur** | hiçbir şey — boşluk sayılı kalır |
>
> **ÖNERİM (a)** — 3,34 sn, deterministik, CI'da araçlar zaten var, ve `check`'in
> **kendi son adımının** (`git diff --exit-code`) bugün ne için var olduğunu ilk kez
> doğru kılar. ⚠️ **Kapsam dışı: `Makefile`'a dokunmadım.**
>
> **B4** — kardeş dosyadaki *"non-deletable without a red test"* başlığı
> **daraltıldı**; B2 sayesinde o cümle **bugün de** ölçülerek yanlış.
>
> **N1 · N3** — iki bayat cümle ölçüme eşitlendi: ağın türetimi artık `q.X(` **metni**
> değil **AST**'dir (ve `store.New(tx).X(...)` **kapsam içindedir** —
> `ledger.Decision` tam olarak onu yazıyor) · ve *"templ yorumları üretilen Go'ya
> taşımıyor"* **yanlıştı**: `<!-- … -->` `templruntime.WriteString`'e **giriyor**
> (yani **tarayıcıya da gidiyor**); ağı koruyan şey **çıkarma değil ayrıştırma** —
> bir `BasicLit` asla `SelectorExpr` değildir.

> **Kart düzeltmesi 9 (2026-08-07, 9. tur — KULLANICI KARARI: B3 → seçenek (a)).**
>
> ⚠️ **KAPSAM GENİŞLEMESİ, AÇIKÇA İŞARETLİ** (sabit kural 8): `Makefile`'ın `check`
> hedefini değiştirmek **M6-04'ün kabul kriterlerinde yok**. Kullanıcı kararı
> (2026-08-07), 8. turda ölçülüp önüne konan iki okumadan (a)'yı seçti. Gerekçe
> ölçümdü: `check`'in **son adımı** *"üretilen dosyalar commit edilmiş mi"* diye
> kontrol ediyordu ama `check` **hiç üretmiyordu** — yani hiçbir zaman üretmeden
> doğruluyordu, ve bayat bir `_templ.go` **CI'dan geçiyordu**.
>
> **DEĞİŞİKLİK:** `check: fmt lint test` → **`check: fmt gen lint test`**.
>
> **SIRA ÖLÇÜLDÜ, VARSAYILMADI.** `fmt` → `gen`, çünkü `templ fmt` **.templ
> KAYNAĞINI** biçimlendirir: önce biçimlendirip sonra üretmek, üretilenin daima
> commit'lenen kaynakla eşleşmesini sağlar. `gen` → `test`, bayat üretimle test
> koşulmasın diye. İki soru ayrıca ölçüldü: **sqlc çıktısı zaten `gofmt -s` temiz**
> (`gofmt -l -s internal/store/` → boş) ve **`templ fmt` bugün hiçbir `.templ`'i
> değiştirmiyor** — yani bu sıra bugün ek fark üretmiyor. Gerekçe `Makefile`'ın
> içine, hedefin yanına yazıldı.
>
> **KANIT 1 — denetçinin senaryosu, YENİ `check`'e karşı.** `admin.templ` değişti
> (`grep -c '@layout.Panel(' → 0`), `make gen` **elle koşulmadı**, üretilen dosya
> **bayat** (`grep -c 'layout\.Panel(' → 1`) → **`make check` exit=2**:
> ```
> --- FAIL: TestLayoutShells_EveryOneIsActuallyRendered (0.19s)
>     dashboard_test.go:1869: layout.Panel is exported and documented but
>     NOTHING outside the layout package renders it.
> ```
> ve `check`'in `gen` adımı üretilen dosyayı **tazeledi** (sonra `grep -c` → **0**).
> 8. turda **aynı senaryo yeşildi**.
>
> **KANIT 2 — sahte kırmızı yok.** Temiz ağaçta `make fmt gen` sonrası
> `git status --short | shasum` **birebir aynı** (`3593b2b7…` → `3593b2b7…`), yani
> `gen` `git diff --exit-code`'a yeni bir fark **eklemiyor**.
>
> ⚠️ **SÜRE İDDİAM DÜZELTİLDİ — VE KARARI SÜREN SAYI BUYDU.** 8. turda `make gen`'i
> **3,34 sn** diye önünüze koydum. 9. turda yeniden ölçtüm: **9,39 / 13,75 / 14,89 sn**
> (üç sıcak koşu) ve **27,86 sn** (`fmt gen`, soğuk önbellek). Fark araçların
> kendisidir — templ ve sqlc `go run <modül>@sürüm` ile koşuyor, yani Go'nun derleme
> önbelleği boşaldığında ikililer **yeniden derleniyor**. **3,34 sn en sıcak
> okumaydı ve tek başına yanıltıcıydı**; planlanacak rakam **~10–15 sn**. Kararın
> yönü değişmiyor (`make check` zaten ~2 dakika ve `test` onun neredeyse tamamı), ama
> *"%2,3"* rakamı **geri çekiliyor**. Sayı-etiketi sınıfının **on birinci** vakası ve
> yine **benim kendi ölçümümde** — bu kez *"bir kez ölçüp aralık yazmamak"* biçiminde.
> Sabit `Makefile`'da koşuluyla birlikte yazılı.
>
> ⛔ `.github/workflows/ci.yml` **DEĞİŞTİRİLMEDİ** (kapsam dışı). Düzeltme davranışsal
> olarak CI'a da geçiyor çünkü CI `make check` çağırıyor; o dosyanın **yorumunun**
> hâlâ yanlış olması orkestratörün backlog'unda.
>
> **Yorum ölçüme eşitlendi (SEKİZİNCİ yorum turu).** `dashboard_test.go`'nun tazelik
> bloğu artık *"`make check` bunu yakalar"* diyor — **ve bu kez cümle yazılmadan önce
> mutasyonla kanıtlandı**; doğrulayan komutlar ve gerçek çıktı yorumun içinde.
> ⚠️ Sınır da yazılı: yakalayan şey **`make check`**, tek başına bu test değil — bayat
> bir ağaçta elle `go test` koşmak hâlâ yeşil verir.

> **M6-04'ÜN SAYILI LİMİTLERİ — tek yerde, kapanışta (2026-08-07).**
> Kapanış kuralı gereği bunların hiçbiri *kapatıldı* diye yazılmadı. ⚠️ Orkestratör
> **13** madde saymıştı; doğrusu **16** — 8. ve 9. turlar iki tanesini *ölçülmüş*
> hâle getirdi (savunulan → ölçülen) ve üçünü ekledi, hiçbiri kapanmadı. Kısaltmak
> yerine düzeltildi.
>
> **Kuşak ağı (§4.5'in ikinci savunması)**
> 1. **Yansımayla adlandırılan sorgu ağdan kaçıyor — ÖLÇÜLDÜ** (8. tur, B2): derlenen,
>    `gofmt`/`vet` temiz bir örnekle yüklem silikken iki belt de yeşil.
> 2. **Başka bir pakette yapılan çağrı** bu paketin AST'sinde yok.
> 3. **Kapsam 41 sorgunun 8'i (%20).** Kalan 33 için ikinci savunmanın **hiçbir ağı
>    yok** (`InsertTransaction`, `GetLastOpenTransaction`, `CreateAdminSession`, …).
> 4. **Satır-sabitleme teklik kanıtı değil:** başka bir dosyada gerçekten satır başında
>    duran aynı `-- name:` yine önce bulunur (sqlc bunu kendisi reddeder, ama okuyucu
>    kontrol etmiyor).
>
> **Kabuk / CSP**
> 5. `@PanelShellWithScript` yazmak **derleniyor**; onu durduran şey **bir test**,
>    derleyici değil.
> 6. Tazelik kapısı **bildirimleri** kapsıyor, **gövdeleri** değil; gövdeleri artık
>    `make check` yakalıyor (9. tur) — ama **elle `go test`** bayat ağaçta hâlâ yeşil.
> 7. `TestLayoutShells_…` **`Wordmark`'ı da kabuk sayıyor** (zararsız aşırı kapsam).
> 8. `.templ` içindeki `<!-- … -->` **üretilen Go'ya giriyor ve tarayıcıya gidiyor**.
>
> **Ürün davranışı**
> 9. **Verilen karar geri alınamaz ve ürün içinde telafi yolu yok** — ADR 0009.
> 10. Onay bandosu kararın **var olduğunu** kanıtlıyor; **kimin** ve **ne zaman**
>     verdiğini değil.
> 11. **Toplu onay yok** (bilinçli).
> 12. Ekran, kararın **kalıcı** olduğunu basmadan önce **söylemiyor** (ADR 0009'un
>     3. seçeneği).
>
> **Veri / sorgu**
> 13. `NOT IN`'in hash'i **`work_mem`**'e bağlı; review kümesi §4.3 gereği **yalnız
>     büyür**.
> 14. **Bileşik FK** ulaşılabilir hiçbir INSERT'te ateşlenmiyor (tetikleyici önce
>     koşuyor); güvenlik denetçisi onu ancak tetikleyiciyi kapatarak gördü.
> 15. **CSV kaçışı M6-07'nin işi** — templ kaçışı oraya geçmez (`= + - @` öneki).
>
> **Yöntem**
> 16. **Gerçek tarayıcıda hiçbir şey açılmadı**; tüm kanıt gerçek HTTP + gerçek
>     Postgres + render edilen markup.
>
> ⛔ Kapsam dışı ve orkestratörün backlog'unda: `.github/workflows/ci.yml:88-90`'ın
> yorumu hâlâ `make check`'in `gen` koştuğunu ima ediyordu — **davranış 9. turda
> düzeldi, o dosyanın cümlesi düzelmedi.**

---

## M6-05 — Employees sekmesi

- **Bağımlılık:** M6-02 · M5-02
- **Commit:** `feat(dashboard): add employees management`

**Kabul kriterleri.**
- Liste: isim, lokasyon/departman, durum (`invited|active|deactivated`), oturum
  durumu (aktif cihaz var mı, son kullanım).
- Aksiyonlar: davet et, yeniden davet, deaktive et, lokasyon/departman değiştir.
  *(B fazında sevk edildi, 2026-08-07 — gerekçe ve ölçümler aşağıdaki 9b bloğunda.
  "Yeniden davet" ayrı bir rota değil: aynı POST'un ikinci kez basılması, ve etiket
  kişinin DURUMUNDAN türetiliyor — sınırı 9b md.11(iii)'te yazılı.)*
- **⛔ Deaktivasyonun ürün içinde geri dönüşü YOKTUR** →
  [ADR 0010](../adr/0010-deaktivasyon-tek-yonlu.md). Ekran bunu butondan ÖNCE
  söylüyor (iki adımlı onay, script yok).
- **Deaktive → sonraki tap `reject`.** Bunu sağlayan şey `employees.status` yazımı
  ve `sys:employee-deactivated` guardrail'idir; **oturum İPTAL EDİLMEZ**.
  `revoked_at` kayıp/çalıntı telefon ve ikinci-aktivasyon içindir.
  *(Kriter 2026-08-07'de düzeltildi — gerekçe aşağıdaki blokta.)*
- Her aksiyon `audit_log`'a yazılıyor.
- Davet kodu ekranda bir kez gösteriliyor, log'a yazılmıyor.
- **⬅️ M6-03'ten DEVRALINDI: *"burada kim çalışıyor?"* sorusunun tek cevabı artık
  BU SEKME.** M6-03'ün Transactions filtresi bir çalışan listesi (`<select>`)
  basıyordu; **kullanıcı kararı 2026-08-06** onu metin girişi + sunucu tarafı isim
  eşleşmesiyle değiştirdi, çünkü liste **ölçüldü: sayfanın %96'sı** (867.233
  baytın 835.319'u, 7.490 `<option>`) ve **maaş bordrosuyla sınırsız büyüyordu**;
  sayfa `no-store` olduğu için her görüntülemede yeniden iniyordu. Sayfa
  **32.066 bayta** (**−%96,3**) indi. **Kabul edilen bedel, açıkça:** müdür artık
  ismi **bilmek ve yazmak** zorunda — kimin çalıştığını **keşfetme** yeteneği
  Transactions'tan çıktı ve **bu sekmenin işi oldu**. Bu sekme o listeyi
  verdiğinde, filtre kutusunun keşif için bir yolu olmayacağı varsayımı düzelir.
  ⚠️ **Sayfalama/arama olmadan aynı hatayı burada yapma:** aynı bordro burada da
  aynı bayt maliyetini üretir; bu sekme listeyi **sayfalamalı veya aramalı**
  göstermeli. *(Ölçüm ve üç seçeneğin gerekçesi M6-03 kartında.)*
- **⬅️ M6-03'ten DEVRALINDI: ayrılmış personel erişilebilir kalmalı (§4.6).**
  M6-03'ün isim eşleşmesi `status` yüklemi **taşımıyor** ve taşımamalı — kısa
  listeleme seçenekleri tam olarak bu yüzden reddedildi. Bu sekmenin listesi de
  `deactivated` olanları **gizlememeli** (filtrelenebilir olabilir, ama varsayılan
  gizleme kayıtları ulaşılamaz kılar). Ağ: `TestPanelTransactionsDB_NameFilterFindsPeopleWhoHaveLEFT`.

> **Kart düzeltmesi (2026-08-07, M6-05 A FAZI uygulaması sırasında).**
>
> **GÖREV İKİYE BÖLÜNDÜ — A: LİSTE (bu tur, sevk edildi) · B: DÖRT AKSİYON (açık).**
> Ölçüt kapsam değil **denetim merceği** (agent-brief, M5-02 ve M6-01'de iki kez
> işe yaradı): okuma yolu §4.5/§4.6 ve **bayt maliyeti** ile denetlenir, yazma yolu
> §4.7/yetkilendirme/oturum ölümü ile. **A fazının karşıladığı kriterler:** liste
> (isim · lokasyon/departman · durum · oturum durumu) ve **sınırlanması**.
> **B fazına kalan, tek tek:** davet et · yeniden davet · deaktive et ·
> lokasyon/departman değiştir · her aksiyonun `audit_log` satırı · davet kodunun
> **bir kez** gösterilmesi · *"deaktive → oturum o saniye ölür"*. A fazı bu
> aksiyonlar için ekrana **buton bile koymadı** ve bu bir testle tutuluyor
> (`TestEmployeesSection_OffersNoWriteAtAll`: sayfadaki tek POST formu oturum
> kapatmadır, ve hiçbir basılabilir kontrolün etiketi dört fiilden birini
> içermez).
>
> **1. Okuma yolu HİÇ YOKTU — M6-03'ün durumunun aynısı.** `db/queries/employees.sql`
> **yalnız iki** sorgu tanımlıyordu (`GetEmployeeActivationContext`,
> `GetEmployeeForTap`) ve **ikisi de tap yolundan**. Panel listesi sıfırdan yazıldı
> (`ListPanelEmployees`). **Migration YAZILMADI ve gerekmedi:** `tappa_app`'in
> `employees` üzerinde SELECT yetkisi var (00003), `sessions_tenant_idx
> (tenant_id, employee_id)` LATERAL'in aradığı indeks, ve `employees_tenant_idx`
> listeyi taşıyor.
>
> **2. 📏 SINIRLAMA — ÜÇ YOL ÖLÇÜLDÜ, KARAR KULLANICININ.** Kart *"sayfalamalı
> **veya** aramalı"* diyor; bu bir seçim ve **A fazı ikisini birden sevk etti**,
> çünkü üçünün de sayıları önce ölçüldü. Gerçek handler, gerçek Postgres, DB'nin
> **en büyük tenant'ı** (kadro sayısı **bilerek yazılmıyor** — her `make test`'te
> büyüyor, bkz. §3'ün kayma uyarısı; sorgusu `adminratelimit.go`'da):
>
> | Yol | Sayfa boyu | Bordroyu baştan sona gezmek | Keşif |
> |---|---|---|---|
> | (a) yalnız keyset sayfalama | **22.028 B** (25/sayfa) | **300'den fazla istek** → #301'de **429** | var (gezerek) |
> | (b) yalnız arama, hepsini basarak | **6.316.252 B** (6,3 MB) | 1 istek | var ama bedeli 6,3 MB |
> | (b′) yalnız arama, aramadan önce boş | ~4 KB | — | **YOK — M6-03'ün deliğini yeniden açar** |
> | (c) ikisi birden **(SEVK EDİLEN)** | **40.103 B** (50/sayfa) | **300'den az istek** veya **1 arama** | var |
>
> (b) satırı M6-03'ün kaldırdığı **867.233 B**'lik kontrolün **7,3 katı** ve sayfa
> `no-store`. (b′) müdürü ismi **bilmeye** zorlar — devralınan borcun ta kendisi.
> **(c) üstkümedir**, yani kullanıcının kararı bir **silme** olur: sayfalamayı
> silmek (b) demektir, arama kutusunu silmek (a). **Sayfa boyu levyesi ölçüldü**
> (aynı sonda, yalnız `ledger.RosterPageSize` değişerek — her satır: belge · kartlar
> · kabuk · yürüyüş):
>
> | `RosterPageSize` | belge | kartlar | kabuk | yürüyüş |
> |---|---|---|---|---|
> | 25 | 22.028 B | 18.036 B | 3.992 B | >300 |
> | **50 (SEVK EDİLEN)** | **40.103 B** | 36.111 B | 3.992 B | **<300** |
> | 100 | 76.253 B | 72.261 B | 3.992 B | — |
> | 500 | 365.471 B | 361.478 B | 3.993 B | — |
>
> **✅ SAYFA BOYU 50 — KULLANICI KARARI 2026-08-07.** Kararı süren şey tek bir sınır
> vakası: 25'te en büyük tenant'ın müdürü kendi kadrosunu **bitiremiyor** (bütçeyi aşan istek sayısı → **#301'de 429**, listenin sonuna
> varılamıyor ve arkadaki herkes gezinerek **ulaşılamaz** kalıyor); 50'de sığıyor. Bedeli açıkça kabul
> edildi: sayfa **+%82** (22.028 → 40.103 B), yine de 867 KB'lık kontrolün **1/21'i**.
> ⚠️ `ledger.PageSize = 25` **ayrı sabit kaldı** ve bağlanmadı: bir gün-listesi
> **trafikle**, bir kadro **bordroyla** sınırlıdır.
>
> **3. 📏 BÜTÇE — YENİDEN SAYILDI, ÇARPAN GELMEDİ; ÜÇÜNCÜ PAYDA DOĞDU VE SABİTİ O
> BELİRLEDİ.** Gerçek sunucu + gerçek oturum, M6-02'nin yöntemiyle: **300 servis
> edildi, ilk `429` tam #301'de** → **görüntüleme başına 1,000 ücretli istek** (bölüm
> script yüklemiyor, fragment rotası yok). Yeni payda **bordro-yürüyüşü** =
> `ceil(E / RosterPageSize)`. Gerçek dağılıma karşı — ⚠️ **payda "DB'deki tüm
> tenant'lar" DEĞİL**, `GROUP BY tenant_id` gereği **en az bir çalışanı olan**
> tenant'lar; ikisi arasında ~1,5 kat fark var ve bu satırın ilk hâli tam olarak
> N7'nin düzelttiği etiketi tekrarlıyordu. Yüzdelikler bu yüzden **koşulludur**.
> Sayılar bilerek yazılmıyor (ikisi de kayıyor); üreten sorgu `adminratelimit.go`'da:
>
> | | 25'te | **50'de (sevk edilen)** |
> |---|---|---|
> | medyan · p95 · p99 | 1 · 1 · 1 | 1 · 1 · 1 |
> | en büyük kadronun yürüyüşü | **>300 istek** → #301'de `429` | **<300 istek** ✅ |
> | ≥15 eşiğini aşan tenant | 1 | 1 |
> | **300 bütçesini aşan tenant** | **1** | **0** ✅ |
>
> ⚠️ **EN BÜYÜK KADRO BİR SAYI DEĞİL, BİR SAAT:** simüle-gün fixture'ı her
> `make test`'te o tenant'a işe alım yapıyor (`seedflow_db_test.go` →
> `insertEmployee`, `fixtures.TenantKF`) ve `employees`'te DELETE yetkisi yok →
> koşu başına ~10 büyüyor. **M6-05 incelemesi boyunca ALTI kez değişti**, biri tek
> bir denetimin içinde iki kez. M6-04'ün *"süitin kendi kirlettiği bir büyüklüğe
> sayı bağlama"* dersi — ve bu görevde **üç ardışık turda bloklayan** oldu, çünkü
> her turda cevap *"sayıyı tazele"* idi. Dördüncü turda cevap değişti:
> **sayıyı hiçbir gerekçe cümlesinde yazma.** Ağı:
> `TestComments_DoNotQuoteTheDriftingRosterSize`.
> **Sabit `adminSessionLimit` DEĞİŞMEDİ**; türetme `adminratelimit.go`'da tavanların
> yanında, **paydası ve üreten sorgusuyla** yazılı. **İnvaryant artık pinli:**
> `RosterPageSize × adminSessionLimit ≥ rosterDesignCeiling` (15.000 ≥ 10.000) —
> `TestRosterPageSize_KeepsAWholeRosterInsideTheSessionBudget`, ve **25'te kırmızıya
> döndüğü ölçüldü**. ⚠️ Bu ağ yazılmadan önce sabiti 50'den 25'e çekmek **hiçbir
> testi kırmıyordu**.
>
> **4. CSP — GENİŞLEMEDİ, KARDİNALİTE PİNİ 1'DE KALDI.** Bölüm `pages.PanelShell`
> render ediyor (script yuvası **yok**, kullanıcı kararı 2026-08-06), sayfa çevirme
> düz bir `<a href>`. Ölçüldü (gerçek handler, bölüm başına başlık):
> `/admin` **8 direktif** (`script-src`+`connect-src`), diğer **beş** bölüm — Employees
> dahil — **6 direktif**, `default-src 'none'` tabanlı. HTMX'in bedeli tartıldı ve
> **alınmadı**: istek maliyeti iki şekilde de aynı (sayfa başına 1), farkı yalnız
> **3.992 B**'lik kabuk (sayfanın %18,1'i), karşılığında ikinci bir script'li URL
> ve ikinci bir CSP olurdu.
>
> **5. §4.7 — LİSTENİN GÖRMEDİKLERİ.** Sorgu `sessions.token_hash`,
> `sessions.device_info`, `sessions.id` ve `employees.email` **seçmiyor**;
> `ledger.Person` ve `components.RosterRowView`'da alan **yok**;
> `employee_invites` tablosuna **hiç JOIN yok** (davet kodu B fazının ve *"bir kez"*
> kuralı var). Oturum hakkında ekrana çıkan tek şey **canlı cihaz SAYISI** ve
> **son kullanım**. Gerçek Postgres'te gerçek bir `token_hash` ve gerçek bir cihaz
> etiketi ile ölçüldü (`TestPanelEmployeesDB_ShowsNoSessionSecret`), ve cihaz
> etiketini uçtan uca sızdıran mutasyon **KIRMIZI**.
>
> **6. ✅ ÇELİŞKİ ÇÖZÜLDÜ — KOD HAKLI, KRİTER DÜZELTİLDİ (kullanıcı kararı,
> 2026-08-07).** A fazı kartın *"Deaktive → oturum o saniye ölür (`revoked_at`)"*
> kriterinin sevk edilmiş bir kod yorumuyla **doğrudan çeliştiğini** ölçtü:
> `db/queries/sessions.sql:83` → *"**DEACTIVATION MUST NOT CALL THIS** … Revoking
> would add nothing to the reject and would push every later tap by that person
> onto the 'revoked' branch, where a caller taking the obvious shortcut writes NO
> record — breaking CLAUDE.md section 4.6."*
>
> **Kararı süren ölçüm, A fazının bulgusu:** o *"obvious shortcut"* bugün üründe
> **alınmıyor** — `internal/httpx/identity.go:176` `ErrRevoked`'ı `SessionRevoked`
> olarak taşıyor ve `Resolved` **dolu** geliyor, yani §5 satır 4 kaydı
> yazılabiliyor. **Ama bu bir garanti değil, bir gözlem:** doğruluğu her çağıranın
> dikkatine bağlı. İptal etmek **sonucu değiştirmiyor** (`sys:employee-deactivated`
> zaten `employees.status`'ü okuyup güvenlik uyarısıyla reddediyor) ama kişiyi
> **sonucun kesin olduğu** daldan (§5 satır 4: reddet + **kaydet** + uyar)
> **doğruluğu dikkate bağlı** bir dala taşıyor. Bedava değil, riskli.
>
> **Düzeltilmiş kriter (yukarıda):** deaktivasyon **`employees.status`** yazar;
> reddi **guardrail** verir; `revoked_at` **kayıp/çalıntı telefon ve
> ikinci-aktivasyon** için kalır. ⚠️ *"Sonraki tap `reject`"* **doğruydu ve
> kaldı** — değişen, onu **neyin** sağladığı.
>
> **↪️ B FAZINA DEVİR — ve bir ölçüm sorusu, karar değil.** B,
> `RevokeSessionsForEmployee`'yi deaktivasyon yolunda **çağırmayacak**. Bunu
> **çağırmadığını** bir ağ pinlemeli mi? A fazı ölçtü ve iki okumayı bırakıyor:
> **(i) Bugün hiçbir şey pinlemiyor.** `RevokeSessionsForEmployee`'nin üretimdeki
> tek çağıranı `internal/session/manager.go`'nun `RevokeAllForEmployee`'si, onun
> da tek üretim çağıranı aktivasyon yolu (`internal/handler/activate.go`, ikinci
> aktivasyon). Deaktivasyon yolu **henüz yok**, yani bugün ihlal edilemez —
> yasağın maliyeti de sıfır. **(ii) Ağın şekli belli ve ucuz:** M5-05'in
> *"çağrıyı pinle"* kalıbı — B fazının deaktivasyon domain'ine sayan bir arayüz
> enjekte et ve *"`Revoke*` **sıfır kez** çağrıldı"* de. Bedeli bir sahte + bir
> iddia. **(iii) Karşı argüman:** bu bir **negatif** iddiadır ve negatif iddialar
> bu depoda sessizce boşalır (M6-01 B'de beş koruma silindi ve süit yeşil kaldı) —
> yani ağ ancak **pozitif kontrolüyle** (aktivasyon yolunun onu **çağırdığını**
> ayrıca ölçen bir test) birlikte anlamlı. **Karar B fazının/kullanıcınındır; A
> fazı mekanizmaya dokunmadı.**
>
> **7. Sayılar.** `make test` (`.env` yüklü, `-race -count=1 -v`):
> **1821 PASS · 0 SKIP · 0 FAIL · 16 paket** — ⚠️ bu sayı **alt testler dâhildir**;
> **üst düzey test sayısı 836** (komut: toplam için
> `go test -race -count=1 -v ./... | grep -c -- '--- PASS:'`, üst düzey için
> `grep -cE '^--- PASS:'`). Turlar: 1813 → 1815 → 1816 → 1819 → 1821. `app.css` **21.230 → 21.334 bayt**
> (**+104**), eklenen **4 seçici** (`.tally--active`, `.tally--invited`,
> `.tally--deactivated`, `.text-base`) ve **hepsi gerçek bir `class=`'a iz
> sürüyor**; sevk edilen **ölü kural 0** — ama düzyazıdaki tek bir küçük harfli
> utility adı bir tanesini **doğurmuştu** ve ölçümle geri alındı (skill
> `tappa-brand`'in tuzağının bu depoda **beşinci** ateşlemesi).
>
> **8. 🔬 MUTASYON GÜNLÜĞÜ — 24 deneme, ÜÇÜ HAYATTA KALDI ve üçü de aynı turda
> kapatıldı.** Hayatta kalanlar burada, çünkü bu deponun dersi *"kapatıldığı iddia
> edilen bir açık, sayılmış bir açıktan tehlikelidir"*:
>
> | # | Mutasyon | İlk sonuç | Ne yapıldı |
> |---|---|---|---|
> | **M4** | iptal edilmiş oturumu **canlı** say (`revoked_at IS NULL` sil) | **YEŞİL** — *"None signed in"* sayfada **başkasının** kartında duruyordu (davetli kişinin hiç oturumu yok) | iddia **kişi bazına** taşındı (`rosterCardFor`) → **KIRMIZI** |
> | **—** | sayfalama testi yalnız **toplamı** karşılaştırıyordu | adı *"ExactlyOnce"* diyordu, ölçmüyordu | **isim çoklu-kümesi** DB ↔ sayfalar karşılaştırması + alfabetik sıra → `>=` cursor, isim-only cursor, id-sıralaması **üçü de KIRMIZI** |
> | **L3-b** | **davetlileri** sona sırala | **YEŞİL** — üç kişilik fixture'da davetli zaten alfabetik olarak sonuncuydu | fixture **her durumdan ikişer kişi** (6) ile örüldü → deactivated · invited · active **üçü de KIRMIZI** |
>
> Kırmızıya dönenler: kadroyu SQL'de/handler'da/şablonda/varsayılan filtrede gizleme
> (4) · durum rozetini kaldırma · cihaz etiketini uçtan uca sızdırma · token
> **önekini** sızdırma (tip duvarı yakaladı) · `templ.Raw` ile kaçırılmamış isim ·
> *"not used yet"* → *"None signed in"* · `+1` satırın silinmesi · kısa sayfa ·
> hiç kimseyi döndürmeyen sorgu (pozitif kontrolü kanıtlar) · `tenant_id`
> yükleminin ana `WHERE`'den ve LATERAL'den silinmesi (§4.5 kuşak ağı) ·
> **sayfa boyu 25'e düşürme** · **500'e çıkarma** · var olmayan rotaya kontrol ·
> phase-B POST formu.
>
> **8b. 🔬 3. TUR — ÜÇÜNCÜ GÖZ RED VERDİ; ÜÇ BLOKLAYAN + SEKİZ BULGU, hepsi
> kapatıldı ve İKİSİ MEKANİZMA DEĞİŞTİRDİ.** Denetçi kendi mutasyonlarını üretti ve
> **iki ağı yendi**; ikisi de aynı turda kapatıldı ve mutasyonla kanıtlandı.
>
> | # | Bulgu | Ölçüm | Sonuç |
> |---|---|---|---|
> | **B1** | Kuşak ağının *"8 QUERIES OF 41"* kapsam iddiası, **kendi bastığı komutla** çürüyordu (gerçek: 42) ve listede `ListPanelEmployees` **yoktu** | `grep -rhoE '^-- name: …' \| wc -l` → **42** | Sayı **düzyazıdan çıkarıldı**: iki büyüklük de zaten çalışma anında hesaplanıyor → test artık kapsamı **loglar** (`8 of 42, %19,0` + adların listesi). Elle yazılan sayı bir daha çürüyemez. |
> | **B2** | Bir ağın gerekçesi **var olmayan bir rota tablosu** beyan ediyordu (*"mountWriting is the only place a POST is registered"*) | `grep -rn 'r\.Post('` → **7 rota**, panelin **4**'ü, **3'ü `Mount` içinde** | Envanter komutuyla birlikte yoruma kondu. ⚠️ Ve daha güçlü düzeltme: *"hiçbir panel GET'i mutasyon yapmaz"* **de yanlıştı** — `TouchAdminSession` her istekte `last_used_at` yazar. Doğru cümle: hiçbir panel GET'i **alan (domain) durumunu** değiştirmez. |
> | **B3** | Aynı büyüklük için **üç dosyada üç farklı sayı**, ve **sabitin hemen üstündeki blok** geri çekilmiş olanı taşıyordu | 8.718 → 8.818 → 8.878 → **8.918** (denetim sırasında bile oynadı) | Sayı **tek eve indirildi** (`adminratelimit.go`, sorgusu + kayma uyarısıyla). `roster.go`'daki blok artık **eşitsizlikle** konuşuyor, ölçülen kadroyla değil; `employees.go`'nun bayt tablosundan **yürüyüş sütunu silindi** (bayt sütunları kaymaz, o kayardı); test mesajlarındaki sabit sayılar **sabitlerden türetildi**. |
> | **N1** | N+1 gerekçesi **sevk edilmeyen** sayfa boyunda ölçülmüştü (26 = terk edilen 25'in ikizi) ve payı abartıyordu | LIMIT 51'de: taban **3,7–9,2 ms**, LATERAL'li **16,1–18,5 ms**, tek `ListSessionsForEmployee` **0,7–1,2 ms** ×50 = **35–58 ms + 50 gidiş-dönüş** | Tablo yeniden ölçüldü. **Mekanizma da düzeltildi:** sondanın kendisi ucuz (`loops=51`, 0,008 ms), pahalı olan **plan değişimi** — LATERAL'siz `top-N heapsort 36 kB`, LATERAL'li **tüm kadro** `quicksort` (853+224 kB). **Karar değişmedi, payı düzeldi.** |
> | **N2** | 🔴 Kuşak ağı **WHERE'i HİÇ OLMAYAN** bir alt sorguyu göremiyordu — ve hata mesajı görebildiğini söylüyordu | Denetçinin mutasyonu (`LEFT JOIN LATERAL (… FROM sessions …)`, WHERE yok) → paket **ok** | **KAPATILDI.** `unscopedSubqueries`: tenant kapsamlı bir tabloyu okuyan parantezli her blokta **kendi derinliğinde** WHERE aranıyor; tablo listesi `CREATE POLICY`'den **türetiliyor**. ⚠️ İlk sürümü **dördüncü saldırıda yenildi** (iç içe bir alt sorgunun WHERE'i dıştakini maskeliyordu) → `maskNested` eklendi. **6 saldırı, 6 KIRMIZI.** |
> | **N3** | *"böyle bir şekil kurulamadı"* cümlesi **tek düzenlemede** yenildi | `ORDER BY (status='deactivated' AND (SELECT count(*) …) > 100)` → iki paket de **ok** | Cümle ölçtüğüne eşitlendi: **ağın dişi fixture ölçeğine bağlı**. Şekil kartta ve testte **birebir yazılı**. §4.6 ihlali değil (durum filtresi tek istekte buluyor) — **keskinlik** boşluğu, sayıldı. |
> | **N4** | `nonSurfaceGrounds["line"]` gerekçesi bayat (*"inside .stamp only"*) | `.tally--manual` **HEAD'de zaten** `docket.templ:106`'da | Gerekçe düzeltildi. ⚠️ Ve sınıf yazıldı: M6-04 aynı girdinin `tomato` ikizini düzeltip `line`'ı süpürmemişti — *kendi vakasını düzeltip kendi sınıfını düzeltmeyen düzeltme*. |
> | **N5** | Referrer-Policy limit notu artık **yanlış URL şeklini** anlatıyordu | *"the name is the reader's own query"* — oysa `?after_name=` **sunucunun DB'den bastığı** bir isim | Not iki tarihli satıra bölündü. İhlal yok (`default-src 'none'`), ama *"yalnız okuyucunun kendi sorgusu"* rahatlığı artık geçerli değil: bir panel URL'i **üçüncü bir kişinin adını** taşıyabiliyor. |
> | **N6** | 🔴 Sonun ötesindeki bir cursor **işletme hakkında YANLIŞ cümle** bastırıyordu | Üç çalışanlı tenant'ta `?after_name=zzz…` → *"No people have been added to this business yet"*, 0 kart | **DÜZELTİLDİ.** Boş duruma **üçüncü dal**: *"That is the end of the list"* + çalışan bir **"Back to the start"**. `Narrowed()` cursor'ı hâlâ saymıyor (konum filtre değildir, testle pinli) — iki doğru cümleyi birbirine karıştırmamak için dal eklendi, ikincisi genişletilmedi. **3 mutasyon, 3 KIRMIZI.** |
> | **N7** | *"MEASURED ACROSS EVERY TENANT"* aslında **çalışanı olan** tenant'lar | `tenants` = **111.313**, `GROUP BY tenant_id` paydası = **76.233** | Etiket düzeltildi; yüzdelikler **koşullu** olarak yazıldı. ⚠️ Orkestratörün brief'i de aynı hatayı taşıyordu — sınıf bir adım yukarı da yayılmıştı. |
> | **N8** | *"alfabetik"* **collation'a bağlı**; bu imajda (`postgres:17-alpine`, musl) **bayt sırası** | `datcollate=en_US.utf8` ama `'a' < 'B'` → **f**, `'Zebra' < 'apple'` → **t** | **DÜZELTİLDİ:** sıra iddiası artık **veritabanına soruluyor** (aynı `ORDER BY`), Go'da karşılaştırılmıyor. Sevk edilen keyset her iki collation'da da doğru (satır karşılaştırması ve `ORDER BY` aynı collation'ı kullanır); düzeltilen şey **M8'de yanlış alarm verecek olan testti.** |
>
> **8c. 🔬 4. TUR — RED'in GEREKÇESİ 3. TURUNKİYLE AYNIYDI, ve çare artık YAPISAL.**
> Denetçi 21 saldırı üretti, **7'si kaçtı**; ayrıca F1'in **dördüncü kez** tekrarladığını
> ölçtü. Kırılma noktası şu: üç turdur cevap *"sayıyı tazele"* idi ve üç turda da
> tazelenen sayı tur bitmeden bayatladı.
>
> | # | Bulgu | Ölçüm | Sonuç |
> |---|---|---|---|
> | **F1** | Kayan kadro sayısı **dört dosyada dört değer**; *"tek ev"* kuralını yazan dosya onu **138 satır sonra** kendisi çiğniyordu | inceleme boyunca **ALTI** değer (biri tek denetim içinde iki kez); bugün ölçüm yine farklı | **Sayı hiçbir gerekçe cümlesinde yazılmıyor.** Argüman eşitsizlikten kuruluyor; canlı rakam gerekiyorsa **sorgu** basılıyor, cevap değil. Ağ: **`TestComments_DoNotQuoteTheDriftingRosterSize`** — şekil taraması, pozitif kontrollü, ve **mutasyonla kırmızı**. M6-05'in **beş** ihlali silindi; kalan **9** M6-01/M6-03'ün (her biri `git show HEAD:` ile önceden var olduğu doğrulandı) → **bütçe 9, yalnız düşebilir**, ve her koşuda **loglanıyor**. |
> | **F2** | N7 yarım düzeltilmişti: kod doğru, **aynı diff'in kartı** aynı etiket hatasını taşıyordu (*"75.274 tenant — DB toplamı"*) | `tenants` ≠ `GROUP BY tenant_id` popülasyonu; aradaki fark ~1,5 kat | Kart düzeltildi; payda **koşullu** olarak yazıldı ve **iki sayı da çıkarıldı** (ikisi de kayıyor). |
> | **C2** | `unscopedSubqueries` yorumu *"**must** have a WHERE"* diyordu; **7 kaçış** bunu çürütüyor | 21 saldırı → **14 yakalandı, 7 kaçtı**; üçü bağımsız olarak yeniden üretildi (`public.sessions` · `ONLY sessions` · virgül-join) | 🔴 **KAPATILMADI — SAYILDI** (kapanış kuralı). Yedisi de **adıyla** yazıldı; `public.sessions` en sert olanı, çünkü tarayıcı `sessions`'ı **biliyor**, o kadar ileri okumuyor. **RLS'in birinci savunma olduğu** ve satırları hâlâ durdurduğu yazıldı. |
> | **C2b** | `employees.sql` *"every JOIN restates `tenant_id` in its own ON clause"* — **ağı var mı?** | `locations` JOIN'inden yüklem silindi → `ledger` **ok**, `handler` **ok**, süit **yeşil** | *"Uygulanmıyor — **disiplin**"* olarak yazıldı, mutasyon sonucuyla birlikte. |
> | **M1** | Kapsam **loglanıyor, iddia edilmiyor**: 8'den 5'e düşse hiçbir şey kırmızı vermez | tek fren `len(names) < 4` | Taban değeri **ölçüldü ve elendi**: kapsam bir okuma silinince **meşru olarak** düşer → taban bir değişiklik dedektörü olurdu. **Limit olarak yazıldı.** |
> | **M2** | Boş durumun *"iki DOĞRU cümleden"* gerekçesi eksik dayanaklı: filtre+cursor birlikteyken yalnız **biri** doğru | — | Gerekçe *"doğruluk"* yerine **kullanışlılık** üzerine yeniden kuruldu, ve maliyeti (filtreyi koruyan geri linki) yazıldı. |
> | **M3** | `quicksort 853 kB + 224 kB` kadroyla ölçekleniyor, **kayma etiketi yoktu** | bugün ~1,2 MB | Rakam **mertebeye** çevrildi; F1 sınıfı olduğu yazıldı. |
> | **M4** | `TestEmployeeStatuses_IsTheSchemaVocabulary` **şemayı okumuyordu** — elle yazılmış listeyle karşılaştırıp *"the schema's CHECK admits"* diyordu | 00003'e 4. durum eklense **yeşil** kalıyordu | **Migration'ın CHECK'i parse ediliyor** (küme eşitliği, sıra değil). **2 mutasyon, 2 KIRMIZI** (şema büyür · Go `deactivated`'ı düşürür). |
> | **M5** | Dokunma hedefi ağı `Clear` ve `Next page`'i **hiç görmüyordu** (`sectionBodies` yalnız filtresiz href'i çekiyor) | `Clear`'dan sınıf silindi → panel geneli test **yeşil** | Bu iki kontrolü **birlikte** render eden yeni test. **2 mutasyon, 2 KIRMIZI**; panel geneli test aynı mutasyonda **yeşil** kalıyor (ölçüldü) — yani delik ağdaydı, üründe değil. |
>
> ⚠️ **Bu turda bir onarım da yapıldı:** mutasyon geri-alma sırasında `employees.templ`
> eski bir yedekle üzerine yazıldı ve M2 düzeltmesi **kayboldu**; fark `git status`
> değil **grep** ile yakalandı (dosya izlenmiyor, `git checkout` onu geri getirmez).
> İkisi de elle geri konuldu ve doğrulandı.

> **8d. 🔬 5. TUR — `tappa-security-auditor` RED: GERÇEK BİR §4.6 KUSURU.** Güvenlik
> merceği genel gözün dört turda görmediğini buldu (bu projede **dördüncü** kez).
>
> | # | Bulgu | Ölçüm | Sonuç |
> |---|---|---|---|
> | **R6** | 🔴 **İmleç adı taşıyordu ve ad `maxRosterCursorName`'i aşınca SESSİZCE düşüyordu** → "Next page" 1. sayfayı yeniden veriyor → **sonsuz döngü**, arkadaki herkes gezinerek **ulaşılamaz**. `full_name` canlı şemada **sınırsız `text`**, CHECK yok. | Kendi testimle yeniden üretildi: sayfa sınırındaki **616 rune**'luk ad → **20+ sayfa, 63 kişi için 1050 satır** | **YAPISAL DÜZELTME: imleç artık KİMLİK taşıyor** (`?after_id=<uuid>`), adı sunucu **kendi okuyor** (`GetRosterCursorAnchor`, tenant yüklemli, aynı transaction'da). Uzunluk sorunu **kaynağında** yok oldu — sabit **büyütülmedi, silindi**. |
>
> **İki yazılı iddiam ölçümle yanlışlandı ve ikisi de düzeltildi:**
> *"dropping can only ever show **MORE** of the roster"* → **daha AZ** gösteriyordu
> (düştüğü sayfa okuyucunun zaten gördüğü sayfaydı) · *"it is visible because the
> filter bar echoes the values that took effect"* → **filtre çubuğu FİLTRELERİ
> yankılar, imleci değil**; düşüş tamamen sessizdi. Her ikisi de
> `parseRosterFilter`'da, yanlışlandıkları ölçümle birlikte yazılı.
>
> **Yan kazanç — N5/S3 büyük ölçüde konusuz kaldı:** *"Next page"* linki artık
> **gerçek bir çalışanın tam adını** taşımıyor; tarayıcı geçmişine ve paylaşılan
> ekrana düşen ad yok. Ayrı bir testle tutuluyor
> (`TestEmployeesSection_NoEmployeeNameTravelsInAPagingLink`). Bu, opak/kodlanmış
> token alternatifinin **neden elendiğinin** de sebebi: token adı taşımaya devam
> eder, yalnız okunmaz hâlde — ne uzunluğu ne ifşayı çözer. `full_name`'e CHECK
> eklemek **migration** gerektirirdi ve kullanıcı kararı olurdu; **gerekmedi**.
>
> **Maliyet ölçüldü ve bütçeye eklendi:** çapa okuması **0,44–0,51 ms**, yalnız
> **sayfalı** isteklerde (bölüm görüntülemesi ve filtre değişimi ödemez), ~19–68 ms'lik
> bir sayfanın **~%2'si**. ⚠️ Bu sayıyı SQL yorumuna **ölçmeden önce** *"0,6–1,9 ms"*
> diye yazmıştım; fark, ürünün hiç yapmadığı bir işi (id'yi bulan alt sorguyu)
> ölçüme dahil etmekten geliyordu — **düzeltildi ve hatanın şekli görünür bırakıldı**.
>
> **Mutasyonlar:** adı imlece geri koy (eski 512 sınırıyla) → uzun-ad testi **KIRMIZI**,
> mahremiyet testi **KIRMIZI**. Çapa sorgusundan `tenant_id` yüklemini sil → kuşak ağı
> **KIRMIZI**. ⚠️ **Bu son mutasyonu ilk koşuşumda YEŞİL raporlayacaktım** — mutasyon
> aslında **hiç uygulanmamıştı** (heredoc alıntılaması); ikinci koşuda `anchor present:
> True` + `MUTATION APPLIED` basılıp doğrulandı. M6-03'ün *"betiğin niyetini değil
> ÇIKTISINI raporla"* dersi, bu turda bana ateşledi.
>
> **S2 · İKİNCİ EKSEN (istek başına DB süresi) M6-05 ile güncellendi.** Tablo yalnız
> Transactions'ı anlatıyordu. Kadro sayfası: **19–68 ms** (`tappa_app`, tenant kapsamlı
> transaction, satırlar gerçekten döndürülerek, 6 tekrar) ve ⚠️ **sayfayla değil
> KADROYLA ölçekleniyor** (LATERAL, planlayıcının top-N heapsort'unu tam bir
> quicksort'a çeviriyor). Kabul gerekçesi yazıldı: `300 × ~35 ms ≈ 10,5 sn/pencere`,
> dosyanın **zaten kabul ettiği** 3,6–12,9 sn bandının içinde, ve bütçe **oturum
> UUID'sine** anahtarlı.
>
> **S1 · dokunulmadı** — denetçi *"§4.6 ihlali değil, keskinlik boşluğu, doğru
> yazılmış"* dedi; limit (iii) olduğu gibi duruyor.

> **8e. 🔬 6. TUR — KAPANIŞ. İki bloklayan da düzyazı/kapsam-iddiası katmanında; yeni
> mekanizma işi YOK.**
>
> | # | Bulgu | Ölçüm | Sonuç |
> |---|---|---|---|
> | **B1** | §4.7 ağının yazılı kapsamı: *"prefix/transform … **COVERED ELSEWHERE**, the cover is the type wall"* | Tip duvarı yalnız **YENİ ALAN** gerektiren sızıntıda ateşliyor (`Value string` → KIRMIZI ✅). **Var olan bir alanı yeniden kullanan** yol ona **hiç uğramıyor** — `Person` değişmiyor. | *"COVERED ELSEWHERE"* **tümden kaldırıldı**; kapsam **dar ve doğru** hâliyle yazıldı, denetçinin tek satırlık örneğiyle birlikte. ⚠️ **Benim yeniden üretimim SAPTI ve bu da yazıldı:** ifadenin dört yazımı (skaler alt sorgu · `\|\|` · LATERAL · NULLIF/CASE/array) sqlc v1.28'de `string`/`bool`/`interface{}` tipleniyor ve **derlenmiyor**; Go tarafından yönlendirince derlendi ve **tip duvarı sessiz kaldı** (denetçi haklı) ama bu dosyadaki **üç test kırmızı** oldu. Dürüst cümle: *"var olan alan üzerinden sızıntıyı hiçbir şey garanti etmiyor; başka bir testin yakalaması **şansa** bağlı"*. Üründe sızıntı **yok** (sorgu `token_hash` seçmiyor). |
> | **B2** | Kayan sayı sınıfı, kendini kapattığını ilan eden diff'in **içinde** üç kez tekrar etti | Üçü de bugün bayat (`tenants` 111.313→**114.031**, ≥1 çalışanlı 76.233→**78.232**, kadro 8.918→**9.138**, yürüyüş 353/177→**366/183**) | **Üç alıntı da kaldırıldı**: (a) `adminratelimit.go` payda cümlesi artık *"kabaca üçte iki"* diyor ve **iki sayıyı da yazmıyor**; (b) `employees.sql` *"iki kez yeniden koştu"* ile yetiniyor; (c) karttaki **altı** `353`/`177` yerine `>300`/`<300` (karar zaten eşik geçişiydi). **Dördüncüsünü ben buldum:** `employees.sql`'de *"8 730 rows"* — aynı büyüklük, başka isim; o da kaldırıldı. |
>
> **Ağın sözlüğü genişletildi — ve genişletmenin KENDİSİ ölçüldü.** Her ismi eklemek
> denendi ve **geri alındı**: 30 meşru ölçümü (bir günün kayıtları, pencere başına
> istek, tenant-günü) işaretliyordu ve *meşru düzyazıda ateşleyen bir teli bir
> sonraki kişi siler*. Sözlük artık **yalnız kadro isimleri**.
> 🔴 **ALTI KAÇIŞ ÖLÇÜLDÜ ve yazıldı:** Türkçe ek (`çalışana`/`çalışanı` — Go'nun
> `\b`'si ASCII olduğu için `çalışanın` yakalanıyor, `çalışana` **kaçıyor**) ·
> `rows`/`records` gibi başka şeyleri de adlandıran isimler (bilerek dışarıda) ·
> yazıyla *"nine thousand"* · ismin sayıdan **önce** gelmesi · isimsiz *"tenant: 9138"* ·
> **iki satıra bölünmüş** sayı+isim.
> ⚠️ **BÜTÇE 9 → 6 DÜŞTÜ VE HİÇBİR ŞEY DÜZELTİLMEDİ.** M6-03'ün üç kart satırı artık
> Türkçe ek kaçışıyla **görünmez**. Borç azalmadı, **ağın görüşü değişti** — ve bunu
> yazmak şart, çünkü sebebi kaydedilmeyen bir bütçe düşüşü, sayılmış bir açığın
> sessizce sayılmamış bir açığa dönüşme biçimidir.
>
> **N1 · Kendi doğrulama komutunu basan yorum ÜÇÜNCÜ kez yanlıştı.**
> `transactions.templ` *"iki PanelShell"* diyordu; bu diff **üçüncü** çağrı yerini
> ekledi (`employees.templ`). **Sayı tümden kaldırıldı** — çağrı yeri sayısı her
> bölüm inşa edilince artar, yani takvim hakkında bir olgu, tasarım hakkında değil.
> Tasarım olan **kardinalite** ve o zaten testle pinli.
> **N2 · Uzun-ad testinin gerekçesi düzeltildi.** Denetçi yeniden üretemedi ve haklı:
> ölçüldü ki `'Lambda P049' < 'Lambda Active Person'` **yanlış**, `'Lambda 0049' <
> 'Lambda Active Person'` **doğru** — yani kaçırma **P önekli** eski sürümde oldu,
> bugünkü sayısal adlarla aritmetik tutardı. ⚠️ Ama **yalnız bayt sıralamalı bir
> collation'da**; bu yüzden ampirik yerleştirme kalıyor (M8'de glibc/ICU'da aritmetik
> **yanlış sebeple** geçerdi). **N3 ·** boş `//` ayıracı eklendi.

> **9. Kapatılamayan / kapatılmayan LİMİTLER (sayıldı, kapatıldığı iddia edilmiyor).**
> (i) §4.7 sayfa taraması **tam** sırrı arar; **önek/dönüşüm** onu yenmez —
> yakalayan `roster_test.go`'daki **kapalı alan kümesi**, ve cümle artık bunu
> söylüyor. (ii) **Log'a** sızan sır bu dosyaların görüş alanı dışında (R7 + §7).
> (iii) 🔴 **Alfabetik sırayı KORUYARAK insan gömen sıralama — ŞEKLİ BİLİNİYOR
> ve açık:** `ORDER BY (status='deactivated' AND (SELECT count(*) …) > 100)`. Süitin
> her fixture'ı eşiğin altında olduğu için yeşil kalıyor, gerçek her kadroda
> ayrılmışları sona gömüyor. **Ağın dişi fixture ölçeğine bağlı**; kapatmak
> saldırganın seçtiği eşiği bilmeyi gerektirir. Satırlar erişilebilir kalıyor
> (durum filtresi tek istekte buluyor) → §4.6 **ihlali değil, keskinlik boşluğu**.
> (iv) `phaseBVerb` **sabit listedir** ve *"End employment"* geçer; yük taşıyan iki
> özellik **türetilmiştir** (POST form aksiyon kümesi = `{/admin/logout}`, ve
> sayfadaki **her** aynı-köken `href` aynı router'da çözülmeli). (v) **Alan durumunu
> değiştiren bir GET** hiçbir ağın görüş alanında değil; bugün öyle bir rota **yok**
> — ama bu, 2. turda basılmış rota envanteri üzerine bir **gözlem**, invaryant
> değil. ⚠️ *"Hiçbir GET yazmaz"* demek **yanlış olurdu**: `TouchAdminSession` her
> kimlikli istekte `admin_sessions.last_used_at` yazar. (vi) *"DB'deki EN BÜYÜK
> kadro bütçeye sığıyor mu"* ölçümü **uygulama rolüyle yapılamaz** (RLS + `db.DB`
> havuz vermez) — `Pool()` eklemek §4.5 gerilemesi olurdu; o ölçüm düzyazıda, üreten
> sorgusuyla duruyor, ve yerine tenant içi `ceil(E/RosterPageSize)` **aritmetiği**
> pinlendi. (vii) 🆕 **Kuşak ağının yeni alt-sorgu taraması SQL anlamıyor**: parantez
> içinde `FROM <tablo>` metnini arar. Bir view, bir fonksiyon ya da tanımadığı bir
> ad üzerinden okunan tenant tablosu görünmez; tablo listesi bu yüzden
> `CREATE POLICY`'den **türetiliyor** ki *"tanımadığı"* küme şema büyüdükçe küçülsün.
> (ix) 🆕 **`unscopedSubqueries`'i yenen YEDİ şekil** — `UNION ALL` kolu ·
> üst düzey `JOIN … ON` (iki varyant) · `FROM public.sessions` ·
> `FROM ONLY sessions` · fonksiyondan sonra virgül-join. Hepsi
> `internal/domain/ledger/query_test.go`'de **adıyla** yazılı; **kapatılmadı**
> (kapanış kuralı), satırları durduran **RLS**. (x) 🆕 **JOIN … ON'daki
> `tenant_id` tekrarının hiçbir ağı yok** — ölçüldü, süit yeşil kalıyor; *disiplin*
> olarak yazıldı. (xi) 🆕 **Kuşak kapsamı (8/42) loglanıyor, iddia edilmiyor**:
> düşerse kimse kırmızı vermez; taban değeri ölçüldü ve **meşru düşüşlerde yanlış
> alarm vereceği için elendi**. (xii) 🆕 **Kayan-sayı ağı ŞEKİL tarar, anlam
> değil**: *"dokuz bin"* gibi yazılmış ya da isminden iki cümle uzaktaki bir rakam
> geçer; en ucuz yolda bir tel, kanıt değil. Ayrıca **9 birimlik miras borcu**
> (M6-01/M6-03) kapatılmadı — bütçe olarak sayıldı, yalnız düşebilir.
>
> (xiii) 🆕 **İmleç çözülemezse sessizce 1. sayfa dönüyor.** Artık yalnız ürünün
> **hiç üretmediği** bir id ile olur (bayat yer imi, elle düzenlenmiş URL, yabancı
> tenant) ve cevap **daha fazla** veri — §4.6'nın umursadığı yön. Yine de sessiz;
> `slog.Info` ile sunucu tarafında görünür, ekranda değil. Sayıldı.
> (xiv) 🆕 **Denetçinin yedi kuşak-ağı kaçışından DÖRDÜ** (UNION kolu, iki
> `JOIN…ON`, virgül-join) yalnız **canlı RLS ile durdurulduğu** ölçüldü; ağ
> davranışları tek tek mutasyonla tekrarlanmadı — denetçinin kendi *"doğrulanamadı"*
> notu, olduğu gibi devralındı.
>
> (xv) 🆕 **§4.7 tip duvarı YALNIZ yeni alan gerektiren sızıntıyı yakalar.** Var
> olan bir alanı yeniden kullanan sızıntı ona hiç uğramaz; başka bir testin
> yakalaması o alanın değerinin iddia edilip edilmediğine — yani **şansa** —
> bağlı. Üründe sızıntı yok (sorgu `token_hash` seçmiyor). *"COVERED ELSEWHERE"*
> ifadesi kaldırıldı.
> (xvi) 🆕 **Kayan-sayı ağının ALTI kaçışı** ölçüldü ve adıyla yazıldı (Türkçe ek ·
> `rows`/`records` · yazıyla · isim önce · isimsiz · iki satıra bölünmüş).
> Sözlüğü genişletmek denendi ve **geri alındı** (30 meşru ölçümü işaretliyordu).
> **Bütçe 9→6 düştü çünkü ağın görüşü daraldı, borç azalmadı.**
>
> (viii) ✅ **ARTIK LİMİT DEĞİL — denetçinin *"doğrulayamadım"* dediği üç
> sqlc-nullability ölçümü 3. turda yeniden üretildi** (izole dizin, aynı
> `sqlc.yaml`, sqlc v1.28): `max(...)` → **`interface{}`** · `max(...)::timestamptz`
> → **`time.Time`** (NOT NULL, yanlış — sıfır canlı oturumda pgx tarayamaz) ·
> çıplak `count(*) OVER ()` → **`int64`** (LEFT JOIN NULL üretebilirken, aynı
> arıza) · **sevk edilen şekil** → `int64` + **`*time.Time`** (doğru). Üçü de
> `employees.sql`'in iddia ettiği gibi.

> **9b. 🔬 B FAZI — DÖRT AKSİYON SEVK EDİLDİ (2026-08-07).**
>
> **Ne indi:** üç POST rotası (`/admin/employees/invite`, `/deactivate`, `/move`),
> kişi başına bir **aksiyon kartı** (`?manage=<id>`), ve her aksiyon için bir
> `audit_log` satırı. **A fazının bilerek koymadığı butonlar artık var** ve
> `TestEmployeesSection_OffersNoWriteAtAll` **emekliye ayrıldı**: yerine gelen
> `TestEmployeesSection_EveryControlLeadsSomewhereThatExists` sayfadaki **her POST
> hedefini ve her linki** aynı router'dan sürüyor. Sabit fiil listesi
> (`phaseBVerb`) **silindi** — dört fiilin dördü de artık meşru etiket, ve doğru
> sayfada ateşleyen bir tel bir sonraki kişinin sileceği teldir.
>
> **1. 🔴 DAVET DİKİŞİ ZATEN VARDI VE HİÇBİR ÇAĞIRANI YOKTU.** `invite.Manager.
> IssueAndDeliver` kodu **döndürmüyor**; bir `Channel`'a veriyor, ve
> `ManagerVisibleChannel` ifşayı **önce** `audit_log`'a yazıp sonra bir `LinkSink`'e
> uzatıyor. B fazı **kanal yazmadı**: `panelLinkSink` (istek başına, yığında, tek
> alan, getter yok) uyguladı ve `cmd/tappa`'da bağladı. Ölçüldü — bu satırdan önce
> seamin **ÜRETİM çağıranı yoktu**. ⚠️ *"Hiçbir çağıranı yoktu"* **yanlıştı** (bu
> cümle brief'te de, kartta da, `main.go` yorumunda da öyle yazılmıştı):
> `git grep NewManagerVisibleChannel` → `internal/handler/e2e_db_test.go`, M5-02'nin
> uçtan uca sürdüğü yer. Fark önemli: seam **test edilmiş ve monte edilmemişti** —
> yani M5-04 şekli (yetenek teslim edildi, onaylandı, üründe ölü), kanıtlanmamış bir
> mekanizma değil.
>
> **2. 🔴 DAVET POST'U YÖNLENDİRMİYOR, RENDER EDİYOR — ve bu panelin kendi kalıbını
> BİLEREK bozuyor.** Diğer her yazma 303 veriyor; link bir `Location` başlığında
> yolculuk **edemez** (adres çubuğu, geçmiş, paylaşılan ekran, Referer). Bedeli
> yazıldı: **tarayıcı yenilemesi bir yeniden-POST'tur** ve **yeni bir davet üretir**
> (yeni kod + ikinci ifşa satırı); eski davet kullanılana ya da süresi dolana kadar
> geçerli kalır — iptal ETMİYOR, çünkü iptal `used_at`'i **kullanamaz**
> (`invites.sql` bunu adıyla yasaklıyor) ve kendi işidir.
>
> **"Bir kez" ne demek, ölçülerek:** kodu geri okuyabilecek **hiçbir rota yok**
> (DB hash tutuyor, `Manager` döndürmüyor, hiçbir GET okumuyor).
> `TestPanelEmployeesDB_TheInviteLinkIsShownOnceAndNeverReadBack` dört yeri birden
> ölçüyor — cevap gövdesi (**var**), sonraki kadro sayfası (**yok**),
> `audit_log.detail` (**yok**, ve ifşa satırı **var**), `employee_invites.code_hash`
> (**yok**) — ve **kanarya kendi kendini kanıtlıyor**: taranan tabloda satır yoksa
> test *"bu yokluk hiçbir şey kanıtlamıyor"* diye patlıyor.
>
> **3. 🔴 DEAKTİVASYON GERİ ALINAMAZ — ve bunu hiçbir dosya söylemiyordu →
> [ADR 0010](../adr/0010-deaktivasyon-tek-yonlu.md).** Ürün içinde *reactivate*
> yolu **yok** (üç `UPDATE employees`'in hiçbiri `'active'` yazmaz; davet yolu
> deaktif kişiyi **reddeder** ve bu bir **güvenlik özelliğidir**, `invites.sql`
> yazıyor). Bu yüzden yıkıcı aksiyon **iki adımlı**: ilk adım bir cümle, düz bir
> `GET` linki (script YOK — sunucunun zorlayamadığı bir tarayıcı diyaloğu onay
> değildir). ⚠️ ADR 0009'dan **farkı** ADR'de tabloyla yazıldı: orada geri
> alınamazlık **yapısal** (trigger `tappa_owner`'ı da reddediyor), burada **ürün
> yüzeyinde** (yetki var, sorgu yok).
>
> **4. ✅ OTURUM İPTAL EDİLMİYOR — ve bu artık bir GÖZLEM değil, bir TEST.**
> A fazı iki okumayı bırakmıştı; B fazı **sonucu** pinledi (negatif iddia tek
> başına sessizce boşalır, M6-01 B'de beş koruma böyle silindi):
> `TestEmployeeActionsDB_DeactivationMakesTheNextTapARejectThatIsRECORDED` —
> aktifken tap **kaydediliyor** (pozitif kontrol) → panel deaktive ediyor → **canlı
> oturum 1'de kalıyor** → aynı telefon tap ediyor → `reject` +
> `sys:employee-deactivated` + **kayıt** + **güvenlik uyarısı**. Mutasyon:
> deaktivasyona oturum iptali eklendi → **KIRMIZI**.
>
> **5. 🔴 AUDIT SATIRI İLE ALAN YAZIMI KADERİ PAYLAŞIYOR** (`RecordTx`, tek
> transaction, `internal/domain/tenant/staff.go`). **İki yön de patlatıldı ve satır
> sayıldı:** iz yazılamazsa çalışan **değişmiyor** ve **0** audit satırı kalıyor;
> alan yazımı reddedilirse (ikinci basış, aynı yere taşıma) **0** audit satırı
> yazılıyor. Mutasyon: `RecordTx` hatası yutuldu → **KIRMIZI**.
>
> **6. 📐 YAZMA YOLU `internal/domain/tenant`'A KONDU — ve bedeli AĞIN ÜÇÜNCÜ
> KOPYASI.** CLAUDE.md §3 çalışan iş kurallarını oraya veriyor; `ledger`'a koymak
> ücretsiz olurdu (ağ orada) ama o paketin *"hiçbir store çağrısı SELECT değil
> değildir"* olgusunu öldürürdü. Üçüncü kopya `query_test.go` olarak yazıldı ve
> **taşımadığı şey** (ledger'ın `unscopedSubqueries` taraması) **ölçülerek**
> limit yazıldı. **Kapsam artışı: bu paket 5 sorgu netliyor** (koşuda basılıyor).
>
> **⚠️ VE İLK HÂLİ MUTASYONLA YENİLDİ.** `MoveEmployee`'den **öznenin**
> (`e.tenant_id`) yüklemi silindiğinde ağ **yeşil** kaldı — çünkü aynı `WHERE`
> içindeki `l.tenant_id = @tenant_id` eşleşmeyi tatmin ediyordu. Matcher **özneye
> bağlandı** (`subjectAlias` + alt sorgu maskeleme); aynı mutasyon şimdi
> **KIRMIZI**. ⚠️ Bu, ledger'ın kopyasının *yanlış* olduğu anlamına **gelmez**:
> onun sorgularında her tablo kendi `WHERE`'ini taşıyor, tek-`WHERE`'li
> `UPDATE … FROM` şekli bu görevle geldi.
>
> **7. 📏 BÜTÇE — DÖRDÜNCÜ PAYDA DOĞDU: AKSİYON BAŞINA.** Gerçek router, gerçek
> bütçe, akış ilk 429'a kadar tekrarlandı:
>
> | Akış | Tamamlanan akış | Ücretli istek/akış |
> |---|---|---|
> | bölüm görüntülemesi | 300 | **1,00** |
> | kişinin kartını açmak | 300 | **1,00** |
> | davet / yeniden davet | 300 | **1,00** |
> | taşıma (POST + 303'ü izle) | 150 | **2,00** |
> | deaktivasyon (onay linki + POST + izle) | 100 | **3,00** |
>
> En pahalı akış **3**, eşik **≥15** — beşte biri. Bütçeyi hâlâ **gezinme**
> harcıyor, aksiyon değil. ⚠️ Davetin en ucuz olması bir **güvenlik** sonucudur
> (render, redirect değil), performans değil.
>
> **8. CSP ve kabuk — GENİŞLEMEDİ.** Bölüm `pages.PanelShell` render etmeye devam
> ediyor (**script yuvası yok**); onay adımı ve sayfa çevirme düz `<a href>`,
> aksiyonlar düz `<form method="post">`. Kardinalite pini **1'de** kaldı.
> `app.css` **+55 bayt**, **2 yeni seçici** (`.break-all`, `.pt-4`), **ikisi de
> gerçek bir `class=`'a iz sürüyor**, **ölü kural 0**. Yeni ton **yok** → kontrast
> ağının çağrı yerleri değişmedi.
>
> **9. Aksiyon kartı SATIR BAŞINA DEĞİL SAYFA BAŞINA.** Taşıma formu lokasyon ve
> departman listelerini ister; satır başına kopyalamak M6-03'ün ölçüp kaldırdığı
> kontrolü `RosterPageSize` kez çoğaltırdı. Bedeli: kartı açmak **+1 ücretli
> istek**.
>
> **10. 🔬 MUTASYON GÜNLÜĞÜ — 15 deneme, İKİSİ HAYATTA KALDI, biri gerçek bulgu.**
>
> | # | Mutasyon | Sonuç |
> |---|---|---|
> | M1 | `MoveEmployee`'den **öznenin** tenant yüklemini sil | **YEŞİL** → matcher özneye bağlandı → **KIRMIZI** |
> | M2 | `RecordTx` hatasını yut | KIRMIZI |
> | M3 | `sameOriginGate`'i resolver'ın **arkasına** al | KIRMIZI (*3 resolver okuması, 0 bekleniyordu*) |
> | M4 | aktörü POST gövdesinden oku | KIRMIZI |
> | M5 | aktivasyon linkini logla | KIRMIZI |
> | M6 | taşıma hedefini `location` diye adlandır (filtreyle çakışır) | KIRMIZI |
> | M7 | `done` bandını durumu **kontrol etmeden** bas | KIRMIZI |
> | M8 | `MaxBytesReader`'ı kaldır | KIRMIZI |
> | M9 | deaktif kişiye davet üret | KIRMIZI |
> | M10 | deaktivasyon oturumu da iptal etsin | KIRMIZI |
> | M11 | `status <> 'deactivated'` guard'ını sil | KIRMIZI |
> | M12 | aynı-yere-taşıma guard'ını sil | KIRMIZI |
> | M13 | rota **sabitini** yeniden adlandır | **YEŞİL — ve bu doğru:** form ile rota **tek sabitten** türüyor, ikisi birden taşınır. Gerçek şekil M13b. |
> | M13b | şablona **düz metin** form action yaz | KIRMIZI (*404*) |
> | M14 | `GetPanelEmployeeForAction`'dan tenant yüklemini sil | **davranış testi YEŞİL** (RLS tutuyor), **kuşak ağı KIRMIZI** — reponun kendi tezinin yeniden ölçümü |
> | M15 | kayan-sayı ağının parantezli kolunu `[1-9]`'dan `[0-9]`'a geri gevşet | KIRMIZI (yeni yanlış-alarm kontrolü) |
>
> **2. TURUN MUTASYONLARI — denetçinin üç RED'ine karşı, hepsi aynı turda:**
>
> | # | Mutasyon | Sonuç |
> |---|---|---|
> | B2-a | `MoveEmployee`'yi **`WHERE`'siz `UPDATE`** taşıyan bir CTE'ye çevir | KIRMIZI |
> | B2-b | `GetPanelEmployeeForAction`'a **`WHERE`'siz alt sorgu** ekle | KIRMIZI |
> | B2-c | `DeactivateEmployee`'yi **kapsamsız `WHERE`**'li bir CTE'ye çevir | KIRMIZI |
> | B1 | blok yürüyüşünü tek gövdeye kör et (yani eski sürüme döndür) | yeni kontrolün **üç probe'u KIRMIZI** |
> | B3-a | `CanInvite: true` | KIRMIZI |
> | B3-b | `CanDeactivate: true` | KIRMIZI |
> | N1 | başlığı *"Placement saved"*a geri al | KIRMIZI |
> | N2 | `invite.DeliverInvite`'a linki loglayan satır ekle | KIRMIZI |
>
> **⚠️ VE BU TUR KAYAN-SAYI AĞININ BİR YANLIŞ ALARMINI ÜRETTİ VE DARALTTI.**
> `(roster|kadro|tenant|payroll)\W{0,3}\(\s*\d…` kolu, *"…as well as to a tenant
> (00002)…"* yazan bir yorumu — yani bir **migration referansını** — kadro sayısı
> sandı ve `make check`'i kırdı. Bir kadro sayısı **sıfırla başlamaz**, o yüzden kol
> `[1-9]` ile daraltıldı: sınıf tümden kalktı, tek bir gerçek alıntı bile
> kaçmıyor, ve **iki yönü de kontrol altında** (parantezli gerçek sayı hâlâ
> yakalanıyor · iki migration referansı artık geçiyor). *Meşru düzyazıda ateşleyen
> bir tel, bir sonraki kişinin sileceği teldir* — bu turda o kişi bendim.
> **Bütçe 6'da kaldı**, borç azalmadı.
>
> **10b. 🔴 3. TUR — `tappa-security-auditor` RED: BİR HESAP DEVRALMA YOLU, BİR
> DEKORATİF ONAY, BİR §5 BÜTÜNLÜK KUSURU.**
>
> **S3 · ✅ KAPATILDI — taşıma BAŞKA BİR LOKASYONA ait departmanı kabul ediyordu.**
> Ölçüm (denetçi, gerçek router): *"MOVE venueA with deptB (venueB'ye ait) → 303;
> stored location=venueA department=deptB"*. Tenant sızıntısı **değil**; §5
> doğruluğu: geç kalma **departmanın vardiyasından** hesaplanıyor ve politikalar
> `department/<id>` ile kapsanabiliyor, yani yanlış çift kişiyi **başka bir şubenin
> sabahına** göre yargılatıyordu. Şema engellemiyor (bileşik FK yalnız **tenant**'ı
> bağlıyor) ve açılır liste **tüm tenant departmanlarını** sunuyor — yani ekran o
> seçimi **teklif ediyordu**. Çare `MoveEmployee`'nin JOIN'ine tek yüklem:
> `AND d.location_id = l.id`. Liste **daraltılmadı** (venue seçimiyle birlikte
> daralmak script ister; liste zaten **venue adına göre gruplu**), sınır **ifadede**.
> Ekrandaki cümle iki sebebi de adlandırıyor. **Mutasyon:** yüklemi sil →
> `TestStaffDB_ADepartmentOfAnotherVenueIsRefused` **KIRMIZI**.
>
> **S1 · 🛑 KAPATILAMADI — MIGRATION GEREKİYOR, KULLANICI KARARI BEKLİYOR.**
> Bir daveti harcamak **kardeş daveti emekliye ayırmıyor**, ve kod ayırdığını
> **yazıyordu**. Denetçinin bulgusunu **HTTP üzerinden** yeniden ürettim (denetçi
> `/activate`'i sürememişti; artık panel harness'ı aktivasyon akışını da mount
> ediyor):
>
> | adım | ölçüm |
> |---|---|
> | iki kez "Send invite" | **2** aynı anda geçerli davet |
> | en yeni kodla `/activate` + `/api/activate` | çalışan **`active`** |
> | sonra | **1** davet **hâlâ bekliyor** |
> | **eski** kodla aktivasyon | **BAŞARILI** — ikinci-cihaz yolu |
>
> İkinci-cihaz yolu `RevokeAllForEmployee` çağırır: **gerçek çalışanın telefonu
> düşer**, linki ele geçiren onun yerine trust 100 ile mesai yazar. Kötü niyetli
> müdür gerekmiyor — *"link gelmedi, tekrar bas"* yeterli.
>
> **Neden kapatamadım — ölçüldü, varsayılmadı:**
> ```
> has_column_privilege('tappa_app','employee_invites',<col>,'UPDATE')
>   → yalnız used_at = true; expires_at/code_hash/... = false
> UPDATE employee_invites SET expires_at = now()  → permission denied
> DELETE FROM employee_invites                     → permission denied
> ```
> `used_at`'i iptal için kullanmak **`invites.sql`'in açıkça yasakladığı** şey (o
> damga *"kod kullanıldı"* demek). Yani iptal = **yeni sütun + yeni sütun GRANT'ı**
> = **migration**, ve brief *"migration gerekiyorsa DUR ve sor"* diyor. **Duruyorum.**
>
> **Şimdi sevk edilen (karar gerektirmeyen yarısı):** (1) `employees.go`'daki
> *"or one is used"* **yanlıştı → ölçtüğüne eşitlendi**; (2) davet ekranındaki
> *"the old one keeps working until it is used or runs out"* — **rahatlatıcı yarısı
> yanlıştı** → yerine, linki **üreten ekranda**, açık uyarı: eski linkler iptal
> **edilmiyor**, onları tutan kişi **bu kişi olarak** giriş yapıp telefonunu
> düşürebilir; (3) açık risk **sayıldı**:
> `TestPanelEmployeesDB_ASecondInvitationDoesNotRetireTheFirst` — ve yorumu *"iptal
> indiğinde bu test SİLİNMEZ, TERSİNE ÇEVRİLİR"* diyor. **Mutasyon:** uyarı cümlesini
> sil → **KIRMIZI**.
>
> **🛑 KULLANICIYA ÜÇ SEÇENEK (ölçüldü, karar verilmedi):**
> **(a) İptal sütunu** — `employee_invites.cancelled_at` + `GRANT UPDATE (cancelled_at)`
> + `ConsumeInviteAndActivate`'e `AND cancelled_at IS NULL` + yeni davet üretilirken
> kardeşleri işaretleyen ifade. **Kapatır.** Bedel: migration (00012), üç sorgu
> dokunuşu, ve *"iptal edilmiş davet"* için yeni bir ekran cümlesi.
> **(b) TTL kısaltma** — panel davetleri `IssueParams.TTL` ile ör. 24 sa üretir
> (aralık `[1sa, 30gün]`, kod tarafı, **migration yok**). **Kapatmaz**, pencereyi
> 7 günden 1 güne indirir. Bedel: `defaultTTL` yorumunun *"Pazartesi verilen kâğıt
> davet Pazar günü hâlâ çalışsın"* gerekçesi düşer.
> **(c) Olduğu gibi bırak** — risk sayılı, ekran uyarıyor, test ledger. Bedel:
> yukarıdaki devralma yolu **canlı** kalır.
>
> **S2 · 🛑 KARAR KULLANICININ — onay adımı SUNUCUDA ZORLANMIYOR.** Denetçi ölçtü:
> onay GET'i **hiç istenmeden** `POST /admin/employees/deactivate` → **303 +
> `deactivated`**. Hidden alanlar yalnız id + filtreler; token yok, işaret yok.
> ⚠️ **Asıl bedel cümleydi:** `employeeactions.go` *"POST yalnızca cümleyi zaten
> basmış bir sayfadan ulaşılır"* diyordu — **yanlış**, ve sonraki okuyucu korumanın
> **var olduğunu** sanardı. **Sevk edilen:** iki yorum (handler + `roster.templ`)
> ölçtüğüne eşitlendi, ve davranış **sayıldı**:
> `TestEmployeeDeactivate_TheConfirmationStepIsNotEnforced` (yorumu: *"zorlama
> indiğinde bu test tersine çevrilir"*).
> **İki okuma, ölçümle:**
> **(a) Sunucuda zorla** — tek kullanımlık/imzalı bir onay değeri. Altyapı **var**:
> `logincontext.go` zaten `newCSRFToken` + `adminCookies` ile aynı şekli giriş
> formunda uyguluyor (çerez + form alanı + `subtle.ConstantTimeCompare`), yani
> maliyet ≈ o kalıbın ikinci bir örneği: **yeni çerez adı + TTL + iki yeni failure
> mode** (süresi dolmuş onay → *"tekrar deneyin"*; iki sekme → ikincisi reddedilir).
> **(b) Cümleyi eşitle ve tek adımlı olduğunu yaz** — *o gün* sevk edilen geçici
> hâl. Bedeli: **geri alınamaz aksiyon tek POST uzağında**, ve ADR 0010 ile birlikte
> okunduğunda risk **ikisinin çarpımı**.
> ⚠️ **SONRAKİ TUR BUNU GEÇERSİZ KILDI:** kullanıcı **(a)**'yı seçti (2026-08-08) ve
> kapı **sevk edildi** — aşağıdaki **10c** ve **10d** bloklarını oku. Bu satır tarihsel
> kayıt olarak duruyor; **bugünkü davranış değil**.
> ⚠️ Not: bu bir CSRF sorunu **değil** (`ProtectWriting` origin'i zaten kontrol
> ediyor); mesele **kendi müdürünün** yanlışlıkla/otomatikleşmiş bir istekle
> uyarısız yazması.
>
> **S4 · bloklamayan, SAYILDI.** Origin başlığı **yokken** `Sec-Fetch-Site:
> same-site` kapıyı geçiyor — *"same-site"* tam olarak **aynı sitenin farklı
> origin'i**, yani `ProtectWriting`'in kendi yorumunun tehdit saydığı şey. Bugün
> tarayıcıyla sömürülemez (POST'ta Origin daima gider) ama **bu diff üç mutasyon
> rotasını, biri geri alınamaz, o satırın arkasına monte etti**. Pinlendi:
> `TestEmployeeActions_SecFetchSiteSameSitePassesTheOriginGate` (iki kontrolüyle).
> **Sertleştirme yapılmadı** çünkü `sameOrigin` **çıkışı ve onay kuyruğunu da**
> koruyor — tek kelimelik değişiklik, ama onların turu bu görev değil.
>
> **⚠️ VE BRIEF'İMİN BİR VARSAYIMI ÇÜRÜTÜLDÜ (denetçi):** *"A fazının uzun-ad
> kusurunu tetikleyen veri B'nin davet formundan girer"* — **yanlış**: bu fazda
> `full_name` yazan **hiçbir form yok**. Davet, var olan bir çalışan satırına
> gönderilir; isim girişi hiç sevk edilmedi.

> **10c. ✅ 4. TUR — İKİ KULLANICI KARARI GELDİ (2026-08-08), İKİSİ DE (a).**
>
> ### Karar 1 — **migration 00012 `cancelled_at`; devralma yolu KAPANDI.**
>
> **Kararı süren ölçüm** (3. turda HTTP üzerinden iki uçtan alınmıştı):
>
> | adım | ölçüm |
> |---|---|
> | iki kez "Send invite" | **2** aynı anda geçerli davet |
> | en yeni kodla aktivasyon | çalışan **`active`**, **1** davet hâlâ bekliyor |
> | **eski** kodla aktivasyon | **BAŞARILI** → ikinci-cihaz yolu → gerçek çalışanın telefonu düşer |
>
> ve **neden migration şart olduğu**:
> ```
> has_column_privilege('tappa_app','employee_invites',<col>,'UPDATE') → yalnız used_at
> UPDATE employee_invites SET expires_at = now()  → permission denied
> DELETE FROM employee_invites                     → permission denied
> ```
>
> **Ne indi:** `00012_add_cancelled_at_to_employee_invites.sql` — nullable sütun
> (geri-doldurma **yok**: bir satırın geçmişte iptal edildiğini iddia etmek uydurma
> olurdu) · **sütun düzeyi** `GRANT UPDATE (used_at, cancelled_at)` (tabloya toptan
> UPDATE **verilmedi**) · `resolve_invite_by_code_hash` **DROP + CREATE** ile
> `cancelled_at`'i de döndürüyor (RETURNS TABLE değiştiği için `CREATE OR REPLACE`
> yetmez) + sahiplik/`REVOKE PUBLIC`/`GRANT EXECUTE` üçlüsü **yeniden** kuruldu
> (DROP onları da düşürür — atlanırsa fonksiyon superuser'a ait olur, ADR 0002 md.7'nin
> yasakladığı genel bypass) · `tappa_resolver`'a `SELECT (cancelled_at)`.
> **`-- +goose Down` gerçekten çalışıyor** — `make migrate-down` koşuldu: sütun
> gitti, fonksiyon 5 sütunlu hâline döndü, `GRANT UPDATE (used_at)` tek başına kaldı;
> sonra `make migrate` ile geri alındı.
>
> **Beşli bozulmadı** (bu yeni tablo değil, sütun): `tenant_id NOT NULL`,
> `(tenant_id, employee_id)` indeksi, `ENABLE`+`FORCE` RLS, `USING`+`WITH CHECK`
> politikası ve GRANT 00009'da ve **değişmedi** — canlı doğrulandı
> (`relrowsecurity=t relforcerowsecurity=t`). **Append-only trigger'la çarpışma yok**:
> 00009 bu tabloya bilerek trigger koymamış (koruma `REVOKE` ile).
> **İndeks eklenmedi** ve gerekçesi yazıldı (her okuyucu zaten `code_hash` UNIQUE'i ya
> da `(tenant_id, employee_id)` indeksini kullanıyor).
>
> **`used_at` iptal için KULLANILMADI**, ve `cancelled_at` *"kullanıldı"* diye
> **okunmuyor**: üç sorgu ikisini de **ayrı ayrı** eliyor
> (`used_at IS NULL AND cancelled_at IS NULL`). Yeni davet, kardeşlerini
> `IssueAndDeliver`'ın **aynı transaction'ında ve INSERT'ten ÖNCE** emekliye ayırıyor
> (sonra olsaydı yeni satırı da yakalardı).
>
> **🔴 VE BİR MUTASYON İKİ AĞIN ARASINDAN GEÇTİ — ölçüldü, düzeltildi.** Tüketen
> ifadeden `cancelled_at IS NULL` silindiğinde **uçtan uca test YEŞİL kaldı**: Go
> tarafındaki `Lookup` iptal edilmiş kodu `GET /activate`'te zaten reddediyor, yani
> POST hiç ifadeye ulaşmıyor. İki doğru katman, aralarında delik — reponun M5-05'te
> ödediği şekil. İfadenin kendi ağı `internal/db`'ye kondu
> (`TestConsumeInvite_DeadInvites/cancelled`, pozitif kontrollü) ve **o mutasyon
> orada KIRMIZI**.
>
> **Mutasyonlar:** iptal ifadesini hiçbir şeyi eşleştirmeyecek hâle getir → HTTP testi
> **KIRMIZI** *ve* SQL testi **KIRMIZI** (fixture boş kalınca *"vacuous"* diye
> patlıyor) · tüketen ifadeden `cancelled_at IS NULL` sil → SQL testi **KIRMIZI**.
> **Ekran ölçtüğüne eşitlendi:** artık *"The previous link … no longer works"*
> (kaç tanesini emekliye ayırdığını sayarak), ve eski uyarının **yokluğu** da
> iddia ediliyor. `TestPanelEmployeesDB_ASecondInvitationDoesNotRetireTheFirst`
> **ters çevrildi** (`…RETIRESTheFirst`), silinmedi.
> ⚠️ **Yan kazanç:** iptal edilmiş bir kodla gelen istek artık kendi audit sebebini
> yazıyor (`invite.ErrCodeCancelled` → `"cancelled"`) — devralma denemesinin izi.
>
> ### Karar 2 — **onay SUNUCUDA zorlanıyor.**
>
> **Kararı süren ölçüm:** onay GET'i **hiç istenmeden** `POST
> /admin/employees/deactivate` → **303, `status='deactivated'`**; ve ADR 0010 gereği
> geri dönüş **yok**.
>
> **Ne indi:** `logincontext.go` kalıbının **ikinci örneği** — onay ekranı tek
> kullanımlık bir değer üretir, **o kişiye bağlar** (`<token>.<employee id>`,
> HttpOnly çerez, `Path=/admin`), form gizli alanda yankılar, handler
> `subtle.ConstantTimeCompare` ile karşılaştırır. **Tek atımlık**: çerez yazmadan
> **önce** silinir, yani yeniden gönderim aynı onayı kullanamaz. **10 dakika** sonra
> tarayıcı göndermeyi bırakır → süre **yokluğa** dönüşüp sunucuda uygulanır.
>
> **İki yeni başarısızlık modu, ikisi de ekranda ve ikisi de ayrı kelime:**
> `confirm-required` (onaysız **veya** süresi geçmiş — sunucudan bakınca **aynı
> yokluk**, hangisi olduğunu iddia etmiyoruz) · `confirm-stale` (**ikinci sekme**:
> tarayıcı başka kişiyi onaylıyor). **Reddedilen dalda DB'de hiçbir şey kalmıyor** —
> ne `employees`, ne `audit_log` (ölçüldü).
>
> **Mutasyonlar:** kapıyı sil → **KIRMIZI** (*"an unconfirmed POST reached the domain
> 1 time(s)"*) · bağlamayı sil → **KIRMIZI** (*"a confirmation bound to another person
> deactivated somebody"*) · tek-atımlık silmeyi kaldır → **KIRMIZI** (*"a replayed
> confirmation wrote a second deactivation"*). Pozitif kontrol testin içinde: onaydan
> **geçen** akış çalışıyor, ve uyarı cümlesi hâlâ ekranda.
> `TestEmployeeDeactivate_TheConfirmationStepIsNotEnforced` **ters çevrildi**
> (`…IsENFORCED`, beş dal).
>
> ### Dördüncü yorum turu — ve bu kez **grep'le** doğrulandı
> `employeeactions.go` · `roster.templ` · `employees.go` (davet etiketi) ·
> `employees.templ` · **ADR 0010** ölçtüğüne eşitlendi. Tarama:
> *"only ever reached from a page"* · *"courtesy rather than a gate"* ·
> *"cancel the earlier ones"* · *"both valid until they expire"* → **üründe hiçbir
> yaşayan iddia kalmadı**; kalan eşleşmelerin hepsi ya **tarihçeyi anlatan**
> düzeltmenin içinde, ya da eski cümlenin **yokluğunu** iddia eden testte.
>
> ⚠️ **Bir limit kapandı, biri kalıyor:** ADR 0010'un *"onay zorlanmıyor"* limiti
> silindi; yerine kalan dar cümle: kapı uyarının **sunulduğunu** garanti eder,
> **okunduğunu** etmez.

> **10d. 🔴 5. TUR — ÜÇÜNCÜ GÖZ RED: İKİ BLOKLAYAN, İKİSİ DE "SEVK EDİLEN ŞEY
> YAZILANI YAPMIYOR".**
>
> **R1 · `ErrCodeCancelled` TAMAMEN AĞSIZDI — ve silmek KAYDI kaybediyordu.**
> Denetçi önce iddianın **doğru** olduğunu ölçtü (emekliye ayrılmış kod → HTTP 400,
> `activation.failed` / `reason='cancelled'`, kehanet yok), sonra hiçbir şeyin onu
> tutmadığını: `grep -rn "ErrCodeCancelled" --include='*_test.go'` → **0**.
> `TestFailures_AreAudited` **kapalı bir tabloydu** ve yeni sebep **satırsız** sevk
> edilmişti. **Mutasyon:** `case` bloğunu sil → kontrol `default:`e düşüyor, o da
> **`failAttempt` çağırmıyordu** → sunulmuş bir devralma kimlik bilgisi **kayıtsız bir
> 500**: `audit_log` satırı yok, davet bütçesi harcanmıyor, ziyaretçiye *"sunucu
> bozuk"* deniyor. **Tüm paket yeşildi.**
>
> **İki katmanlı düzeltme, çünkü kapalı tablo ağ değil değişiklik dedektörüdür:**
> (1) `switch` bir **tabloya** dönüştü (`inviteFailureReasons`) ve
> `TestActivationReasons_CoverEverySentinel` sentinel kümesini `internal/invite`'ı
> **`go/ast` ile ayrıştırarak TÜRETİYOR** — yani sebep verilmeden eklenen bir sentinel
> **kırmızı**. `ErrUnknownCode` **tek beklenen istisna** olarak adıyla yazılı (tenant
> yok → `audit_log.tenant_id` NOT NULL). (2) 🔴 **`default:` dalı artık KAYDEDİYOR**
> (`reason="unclassified"`) — çünkü o dal §4.6'ya göre zaten yanlıştı: bilinmeyen bir
> hata da kaydedilmeli. Sebep hata **metninden türetilmiyor** (`err.Error()` değer
> alıntılayabilir); sabit bir kelime yazılıyor, ayrıntı process log'una gidiyor.
> **Mutasyonlar:** tablodan `cancelled` sil → **iki test birden KIRMIZI** (türetilmiş
> olan *"listeler ayrıştı"*, tablo satırı *"reason = unclassified, want cancelled"*) ·
> `default:`ten `failAttempt`'i sil → **KIRMIZI** (*"audit events = [], want exactly
> one"*).
>
> **R2 · ONAY KAPISININ YAZILI GARANTİSİ YANLIŞTI — ve kullanıcı kararı "zorla"ydı.**
> Sevk ettiğim şey **saf double-submit çerezti**: MAC yok, sunucu kaydı yok, oturuma
> bağ yok. Denetçi iki satırda forge etti:
> ```
> cookie: tappa_admin_confirm=attacker-chosen-value.<employee id>
> POST /admin/employees/deactivate id=<employee id>&confirm_token=attacker-chosen-value
> -> 303 ?done=deactivated,  employees.status = "deactivated"
> ```
> Yorumum *"logincontext.go'nun ŞEKLİ, BİLEREK"* diyordu — **ve kusur tam olarak
> buydu: şekil mekanizma değildir.** O dosyanın güvenliği sunucu anahtarı altındaki
> **HMAC**'ten geliyor; bende **anahtar yoktu**.
>
> **⚠️ KALIBI KOPYALARKEN PARÇALARINI SAYDIM — bu depoda üçüncü yarım kopya.**
> `adminChoices`'ın **on** parçası `deactivateconfirm.go`'da tek tek listelendi ve
> hepsi uygulandı: kendi etiketiyle **türetilmiş anahtar** · **sıfır değer = hata**
> (mint **ve** parse) · **zorunlu bağlama** (burada **iki**: çalışan **ve oturum**) ·
> sürümlü payload · **uzunluk önekli** MAC girdisi · `base64url(payload).base64url(sig)`
> · sıra **şekil → İMZA → süre → alanlar** · **sabit zamanlı** karşılaştırma ·
> "authenticated but unreadable" = malformed · **sunucu saatiyle** TTL.
> Çerez **kaldı ama işi değişti**: artık yalnız **tek-atımlık** defteri; güveni MAC
> veriyor.
>
> **Denetçinin sekiz saldırısı + forge = DOKUZ, hepsini kendim koştum** → hepsi
> **reddedildi**, alan katmanına **0** çağrı; pozitif kontrol geçiyor.
> **Mutasyonlar:** imza karşılaştırmasını körleştir → **KIRMIZI** (forge saldırısı
> `done=deactivated` alıyor) · oturum bağlamasını kaldır → **KIRMIZI** · süre
> kontrolünü kaldır → **KIRMIZI**. Ayrıca değerin kendi testi: başka oturum · başka
> çalışan · TTL'in bir saniye içi/dışı · sıfır değer · **başka anahtarla imzalanmış**.
>
> ⚠️ **Ve güvenlik kazancı KÜÇÜK — bunu yazmak dürüstlüğün parçası.** Bu kapının
> karşısındaki aktör panelin **kendi oturumlu müdürü** ve o kişi zaten GET-sonra-POST
> yapabilir. Kazanılan şey kullanıcının istediği **garanti**: yazmaya ulaşmak,
> sunucunun uyarıyı **o kişi için, o oturumda, on dakika içinde** render ettiği
> anlamına geliyor.
>
> **Bloklamayanlar (hepsi kapatıldı):** **N1** üç dosyada üç dal sayısı → sayı
> **tümden kaldırıldı** (F1 sınıfı) · **N2** yorumdaki `grep` çıktısını yanlış
> aktarıyordu → komut `^` ile **yeniden üretilebilir** hâle getirildi ve altı-satır
> tuzağı yazıldı · **N3** iptal **süresi dolmuş** davetleri de emekliye ayırıyordu ve
> ekran onları **sayıyordu** → `now() < expires_at` eklendi; *"süresi doldu"* ile
> *"iptal edildi"*yi birleştirmek `used_at` için yasaklanan karıştırmanın aynısıydı.
> **Mutasyon KIRMIZI**, ve ⚠️ o mutasyon **fixture'ımın kendi kusurunu** ortaya
> çıkardı (deterministik `code_hash`, ikinci koşuda 23505 — *bir kez çalışan fixture,
> göstermek için kurulduğu sonucu gizler*) → rastgele yapıldı, **iki kez** koşuldu ·
> **N4** yetim doc comment → yardımcı dosya sonuna taşındı · **N6** kartta bayat
> şimdiki zaman → 3. tur bloğuna *"sonraki tur bunu geçersiz kıldı"* uyarısı kondu.
>
> ⚠️ **Ve orkestratörün bir sayısı düzeltildi:** kuşak paydası **46 değil 47**
> (koşuda: ledger **9/47**, tenant **5/47**).

> **10e. ✅ 6. TUR — KAPANIŞ. Güvenlik denetçisi RED verdi ve açıkça yazdı:
> *"üç bulgunun hiçbiri sömürülebilir değil, hiçbiri kırmızı çizgi çiğnemiyor."***
> **Kapanış kuralı uygulandı: yeni mekanizma YOK; ölçüldü, cümleler düzeltildi,
> kalan LİMİT olarak sayıldı.**
>
> **B1 · ADR'nin *"kanıtlandı"* dediği TEK-ATIMLIKLIK sunucuda tutulmuyor.**
> `a.short.clear` bir `Set-Cookie … Max-Age=-1`, yani istemciye **rica**; sunucuda
> harcanmışlığı tutan hiçbir durum yok. Denetçi her POST öncesi çerezi yeniden basan
> bir istemciyle **tek onayı üç kez** harcadı.
>
> 🔴 **ASIL MESELE AĞDAYDI:** replay testi `browser` yardımcısı üzerinden koşuyordu ve
> **o yardımcı çerez silmeyi uyguluyordu** — assertion sunucunun değil, **test
> istemcisinin işbirliğinin** ölçümüydü. Bu, `activate_test.go`'nun *"kapalı liste ağ
> değil"* sınıfının bir kademe küçüğü ve **sonucu ne olursa olsun düzeltilmesi
> gereken** şey.
>
> **Yapılanlar, bu sırayla:**
> **(1)** İki yeni test **sunucuyu** ölçüyor: `…TheOneShotDependsOnTheClient`
> (işbirliği yapmayan istemci → **3 mint'ten 3 harcama**, ve *"burada 1 görürsen
> defter eklenmiş, bu testi sil ve ADR'yi düzelt"* diye yazıyor) ve DB ikizi
> `TestPanelEmployeesDB_ARepeatedConfirmationWritesOnlyOnce` (ikinci harcama →
> `problem=already-deactivated`, **audit satırı artmıyor**, `deactivated_at`
> **oynamıyor**). Harness'a `setCookie` eklendi — *bir istemcinin bilerek işbirliği
> yapmaması*, sunucunun ne tuttuğunu ölçmenin tek yolu.
> **(2)** Cümleler ölçtüğüne eşitlendi: **ADR 0010** (dört kanıtlanmış özellikten
> *"tek-atımlık"* **çıkarıldı**, yerine tutulmayanı ölçen iki test adıyla yazıldı, ve
> md.3'e ikinci dar cümle eklendi) + `deactivateconfirm.go` + `employeeactions.go`'da
> **üç ayrı yer**. Grep ile doğrulandı: bu görevin dosyalarında *"one-shot"*u **tutulan
> bir özellik gibi** anlatan **tek satır kalmadı**.
> **(3)** ⛔ **Sunucu defteri KURULMADI.** Bir tablo/sütun = altyapı, ve kazanç
> **ölçülmüş sıfır**: aynı aktör tek GET ile taze onay üretir, 2..N harcamalar
> `DeactivateEmployee`'nin `status <> 'deactivated'` yüklemine çarpıp **hiçbir şey
> yazmaz**. **Tutulan** (MAC): *"sunucu bu kişi için, bu oturumda, pencere içinde
> uyarıyı render etti"*. **Tutulmayan:** tek-atımlıklık **istemciye bağlı**.
>
> **B2 · sayılmamış 11. parça — EKLENDİ.** `parse` yalnız `now > issued + ttl` test
> ediyordu; denetçi **bir yıl ileriye** damgalanmış onayı temiz geçirdi. Saldırgan
> erişimi yok (damga sunucunun saati, MAC'in içinde) **ama dosyanın manşeti
> *"parçalar SAYILDI"*** — yani hata tam da dosyanın bitirmek için var olduğu
> sınıftaydı. `adminConfirmFutureSkew = 1 dk` eklendi (adminChoices'ın figürü), liste
> **11**'e çıktı, ve pencere artık **TTL + skew = 11 dk** diye yazılı — her yerde 10
> yazan cümle düzeltildi. **Mutasyon:** skew kolunu kaldır → **KIRMIZI**; tolere
> edilen yön hâlâ geçiyor.
>
> **B3 · teslim başarısız olunca ÇALIŞAN bir link sessizce ölüyor — YORUM DÜZELTİLDİ,
> SAYILDI.** İptal + INSERT transaction'ı commit ediyor, `DeliverInvite` **dışarıda**
> koşuyor; o yazma patlarsa çalışanın elindeki link **emekliye ayrılmış** oluyor ve
> kimse aktive olamıyor. Yorum hâlâ tek sonucun *"davet bekleyen olarak görünür,
> **kayıp değil**"* olduğunu söylüyordu — **00012'den beri eksik**. §4.6 ihlali
> **değil** (emeklilik `cancelled_at`'te kayıtlı, DELETE yok, hata enjeksiyonu
> gerekiyor, ikinci basış **kendini onarıyor**) ve kapatmak teslimi transaction'ın
> **içine** almayı gerektirirdi — `internal/invite` bunu bilerek reddediyor
> (*commit edilmemiş ama teslim edilmiş bir kod, teslim edilmemiş ama commit edilmiş
> bir koddan kötüdür*).
>
> **Ve bir cümle AŞIRI DEĞİL, EKSİK ifade olduğu için düzeltildi:** *"IT IS NOT CSRF
> PROTECTION"*. Token sayfaya render ediliyor, çerez `HttpOnly`+`SameSite=Lax`+
> `Path=/admin` — çapraz-origin okuma SOP ile kapalı, yani Origin kontrolü **Origin
> başlığı yokken düşerse** bu fiilen ikinci katman senkronizatör token'ı olarak
> çalışıyor. Cümle *"panelin CSRF savunması değildir ve öyle kullanılmamalı, ama
> hiçbir şey de değil"* diye yeniden yazıldı.
>
> ⚠️ **Denetçinin MAC'e karşı dokuz forgery denemesinin hiçbiri geçmedi** (anahtar
> karışımı, ham `SessionHMACKey`, etiketsiz, sürüm 0/2/boş, 5/3 alan, employee↔session
> yer değiştirmiş) ve **downgrade yolu yok** (sürüm MAC'in içinde).

> **11. Kapatılanlar ve kapatılmayanlar.**
>
> **✅ (i)+(ii) KAPATILDI — ve ikisi de 2. turda BLOKLAYAN olarak geri geldi, çünkü
> yazdığım limitler YANLIŞTI.** (i) *"Bu kopya `unscopedSubqueries` taşımıyor;
> `TestStaffQueries_ASubqueryWithNoWhereIsInvisible` tarama eklendiği gün kırmızıya
> döner"* — denetçi taramayı **gerçekten yazdı** ve **iki test de yeşil kaldı**: o
> test yalnız kendi literal probe'unu ölçüyordu, bir taramanın **var olup olmadığını
> gözleyecek hiçbir yolu yoktu**. Ölçüm değil, **ölçüm kılığında bir hatıraydı**.
> (ii) *"`subjectAlias` CTE anlamıyor → CTE'nin `WHERE`'i dıştaki özneye göre
> denetlenir"* — **yönü tersti**: gerçek davranış **yanlış-kırmızı** değil
> **yanlış-YEŞİL**di, ve kaçan şey bir **YAZMA**ydı. Denetçinin mutasyonu:
> `WITH moved AS (UPDATE employees SET location_id = @location_id RETURNING …)` —
> **hiç `WHERE`'i yok**, yani rolün gördüğü her satır — dış `SELECT`'in yüklemi
> onun yerine cevap veriyordu → belt **ok**. Üstelik limit *"ledger's copy … handles
> that shape"* diyordu, yani reponun **yasakladığı** *"başka yerde kapatıldı"*
> cümlesi.
>
> **Kapanış kuralı uygulanmadı çünkü KAPATILABİLDİ.** Kontrolün birimi artık sorgu
> değil **BLOK**: `statementBlocks` üst düzeyi **ve** her parantezli bloğu ayrı ayrı
> veriyor (her biri kendi iç parantezleri maskeli), ve tenant tablosuna dokunan her
> blok **kendi öznesinin** kapsam sütununu `@tenant_id`'ye bağlayan bir `WHERE`
> taşımak zorunda. Tablo listesi `CREATE POLICY`'den türetiliyor. **Beş şekil kalıcı
> probe olarak yazıldı** (`TestStaffBlockScan_FlagsTheShapesThatBeatTheOlderCheck`),
> ve sevk edilen üç sorgu **temiz kalmalı** diye ikinci yönü de kontrol ediliyor.
> **Mutasyonlar:** CTE-`WHERE`'siz-UPDATE → **KIRMIZI** · CTE-kapsamsız-`WHERE` →
> **KIRMIZI** · alt sorgu-`WHERE`'siz-okuma → **KIRMIZI** · blok yürüyüşünü tek
> gövdeye kör et → kontrolün **üç probe'u KIRMIZI**. `TestStaffQueries_ASubqueryWith
> NoWhereIsInvisible` **silindi**; delik yok, limit cümlesi de yok.
>
> **✅ (iia) `CanInvite`/`CanDeactivate` AĞSIZDI — kapatıldı.** Denetçi ikisini de
> sabit `true` yaptı, **tam paket iki koşuda da YEŞİL** (`-race`). Ürün doğruydu;
> **üç dosya** (`employees.templ`, `rosterview.go`, `roster.templ`) tutulduğunu
> iddia ediyordu ve adı geçen test **kesinlikle daha zayıf** bir özelliği tutuyor
> (form hedefi servis ediliyor mu). Yeni ağ deaktif kişinin kartını **render ediyor**
> ve üç şeyi birden istiyor: iki kontrol **yok**, iki gerekçe cümlesi **var**, ve
> **taşıma formu duruyor** (§4.6 — pozitif kontrol + kartın boş olmadığının kanıtı);
> ayrıca aktif kişinin kartı ikisini de **sunuyor**. **2 mutasyon, 2 KIRMIZI.** Üç
> cümle ölçtüğüne eşitlendi.
>
> **(iii) **Davet butonunun etiketi
> DURUMDAN türetiliyor**, bekleyen davet olup olmadığından değil — bunu bilmek
> `employee_invites` JOIN'i ister ve A fazı o tabloyu bilerek listeye sokmadı.
> Sonuç: *"Send invite"* iki kez basılırsa **iki geçerli davet** olur, ve ekran
> bunu söylemiyor. (iv) **Deaktivasyonu geri alacak yol yok** → ADR 0010; şema
> izin veriyor, ürün vermiyor, geri almanın **audit izi olmazdı**. (v) **JOIN … ON
> içindeki `d.tenant_id` yüklemi netlenmiyor** (ağ ON'ları yapısal olarak dışlıyor,
> doğru olarak) — orada koruma **bileşik FK**'dir, yani disiplin değil yapı, ve
> `employees.sql` bunu yazıyor. (vi) **Eşzamanlı iki müdür** ölçüldü: deaktivasyon
> 8 goroutine → **tam 1** kazanan, **1** audit satırı. **Taşıma da ölçüldü
> (denetçi)**: 8 → **4 yazma / 4 `no-change`**, **4 audit satırı**, kayıp yok.
> ⚠️ Bunun **ekrandaki bedeli sayıldı ve kapatılmadı**: yarışı kaybeden müdür
> *"That is already where they work"* okuyor — **durum hakkında doğru, niyet
> hakkında yanıltıcı**. Ürün ikisini ayırt **edemez** (form render edildiğinde
> yerleşimin ne olduğunu hiçbir şey kaydetmiyor), o yüzden cümleye ikinci bir satır
> eklendi: *"If you had just changed it, check the placement above…"* — tahmin
> etmiyor, karta işaret ediyor.
>
> (vii) 🆕 **`AdminInviteIssued` CSP karşılıklılık testinin görüş alanında DEĞİL.**
> O test korpusunu `pages.PanelSections`'tan kuruyor; bu ekran bir **POST cevabı**
> olduğu için orada yok. Bugün doğru (aynı `a.render`, aynı dar politika, script
> yok), ama *"her panel ekranı pinli"* demek **yanlış olurdu**. Sayıldı.
>
> (viii) 🆕 **`"unreadable"` ikiye bölündü** (N5): kartın DB okuması patlayınca
> ekran artık *"We could not load this person's details…"* diyor, çünkü *"nothing
> was looked up"* diyen sözlük maddesini paylaşmak müdüre **kendi gönderiminin**
> bozuk olduğunu söylüyordu — patlayan **bizim** okumamızdı. Ayrıca `employees.go`
> yorumunun vaat ettiği *"could not be loaded"* cümlesi **üründe hiç yoktu**;
> artık var.
>
> (ix) 🆕 **§4.7 *"loglanmıyor"* İKİ ayrı iddiaydı ve biri ağsızdı** (N2). Handler'ın
> logger'ı ölçülüydü; ham linki tutan **tek fonksiyon** (`invite.DeliverInvite`)
> değildi — denetçi oraya bir log satırı ekledi, `internal/invite` ve
> `internal/handler` **ok**, `redline-check` **temiz**. Kapatıldı: seam'in kendi
> testi (`TestManagerVisibleChannel_DoesNotLogTheLinkItHolds`, `slog.SetDefault`
> yakalayıcı + anti-vacuity + iki pozitif kontrol), mutasyonla **KIRMIZI**.
> **Kalan limit:** kendi `*slog.Logger`'ını taşıyan bir gelecek uygulama görünmez —
> bugün bu paket logger **enjekte etmiyor**, yani `slog.Default()` tek çıkış.

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

> **Kart düzeltmesi (2026-08-08, M6-06 A uygulaması sırasında).** Görev kullanıcı
> kararıyla iki faza bölündü (ölçüt kapsam değil **denetim merceği**; agent-brief'in
> "Bir görevi A/B fazına bölmek" kaydı). **Faz A** yukarıdaki **1.** ve **6.**
> kriterleri kapsar (lokasyon yapılandırması + departman yönetimi); kalan dördü —
> plaket listesi, replace tag, tag geçmişi, `encoded/pending` — **Faz B**'nindir ve
> kendi migration'ı (00013) + `tappa-db-migrator` turuyla gelir. Faz A `tags`
> tablosuna **hiç dokunmaz** ve **hiç migration eklemez**.
>
> 🔴 **1. kriterin "CRUD"undaki D, ölçüm önüne konduktan SONRA karara bağlandı ve Faz
> A'da sevk edildi.** Şemada silmenin ne anlama geldiği önce ölçüldü (dev DB,
> 2026-08-08, `pg_constraint` + satır sayımı):
>
> - `locations` ve `departments`'a referans veren **altı FK var ve altısı da
>   `ON DELETE RESTRICT`** (`departments→locations`, `employees→locations`,
>   `employees→departments`, `tags→locations`, `transactions→locations`,
>   `transactions→departments`).
> - İki tabloda da **`status`/`archived`/`deleted_at` sütunu YOK** — yani
>   kullanımdaki bir lokasyon ne silinebilir ne de **arşivlenebilir**.
> - Sayılar — ⚠️ **mutlak satır sayıları KAYIYOR, oran kaymıyor.** Dev DB her suite
>   koşusunda büyüyor (testler tenant tohumluyor, `audit_log` append-only). Aynı gün
>   (2026-08-08) **beş** ölçüm: **117 553 · 117 775 · 121 030 · 121 780 · 122 136**
>   lokasyon. Referanslı oranı **beşinde de %84,1**. Bu yüzden karar **orana** dayanır, mutlak sayıya değil;
>   ve mutlak sayılar **yalnız burada**, tarihiyle yazılıdır — kodda hiçbir yerde
>   canlı iddia olarak durmaz.
>   - **Lokasyon (2026-08-08, 5. ölçüm):** 122 136'nın **102 725'i** referanslı
>     (**%84,1 — silinemez**), **19 411'i** referanssız (**%15,9 — silinebilir**).
>     Oran **beş ölçümün beşinde de %84,1** — mutlak sayı 117 553'ten 122 136'ya
>     çıkarken bile.
>   - Tablo başına ayrı ayrı referanslanan lokasyon (2. ölçüm anı): `employees`
>     87 484 · `tags` 68 782 · `transactions` 54 233 · `departments` 15 872.
>   - **Departman:** ⚠️ bu oran sabit **değil** ve **bant da değil — MONOTON DÜŞEN bir
>     seri.** Aynı gün beş ölçüm (referanslı payı): **%91,2 → %89,4 → %87,6 → %86,9 →
>     %86,6**. Yönü belli: **düşüyor**.
>     - **Sebebi bizim kendi suite'imiz.** `venue_db_test.go` her koşuda departman
>       yaratıyor (`Bar`, `Real`, `Legit`, `Kitchen 2`…) ve bunlara **hiç çalışan ya
>       da işlem bağlamıyor** — yani her koşu referanssız departman ekleyip payı
>       seyreltiyor. Lokasyon oranının sabit kalmasının sebebi de aynı madalyonun öbür
>       yüzü: fixture lokasyonları **her zaman** bir departman/çalışan alıyor.
>     - ⚠️ **Bu yüzden bir önceki sürümde yazdığım "%8,8–%12,4 bandı" YANLIŞTI** — 4.
>       ölçüm (%13,1 referanssız) daha yazıldığı gün bandın dışına çıktı. Monoton bir
>       serinin üç noktası **sınır gibi okunur ama sınır değildir**; doğru sunum
>       seriyi **yönüyle** vermektir.
>     - **Karara giren tek şey bir EŞİTSİZLİK:** kullanımdaki departmanların **büyük
>       çoğunluğu** `ON DELETE RESTRICT` yüzünden silinemez. Kesin payı bu veritabanı
>       **ölçemez**, çünkü sayacı biz kirletiyoruz. Son ölçüm: 18 111'in **15 688'i**
>       referanslı.
>
> Yani "D" bugün ya *yalnız yetim satırlar için* mümkün ya da hiç yok. Üç yol da
> ölçüldü, hiçbiri elenmedi:
>
> | Seçenek | Somut sonucu |
> |---|---|
> | **(a) Hiç Delete sunma** | Lokasyonların **hepsi** için kontrol yok. Ekran hiçbir zaman reddetmez çünkü hiçbir zaman teklif etmez. |
> | **(b) Yalnız referanssız satıra Delete** | Lokasyonların **%15,9**'unda çalışır, **%84,1**'inde *niçin* olmadığı sayıyla söylenmeli. Departmanlarda pay ölçülemiyor (yukarı bkz.); söylenebilecek tek şey **çoğunluğun silinemeyeceği**. |
> | **(c) Migration ile arşiv sütunu iste** | Silme yerine emeklilik: kullanımdaki venue de gizlenebilir. **Faz B'nin 00013'üne** ya da ayrı bir karta gider; §4.3'ün ruhu gereği `transactions`'ın işaret ettiği satır **yok edilmemeli**, bu seçenek onu korur. |
>
> ✅ **KULLANICI KARARI (2026-08-08): SEÇENEK (b).** Delete **yalnız referanssız**
> lokasyona ve departmana sunulur. Faz A bunu uyguladı; aşağıdakiler kararın parçası:
>
> - **Referanslı satırda buton HİÇ ÇIKMAZ** ve neden çıkmadığı **sayıyla** söylenir
>   ("2 departments · 3 employees · 1 plaque · 9 recorded taps" — şablon aynı `·`
>   ayırıcıyı kullanıyor, testle pinli). Bir kontrolü gösterip
>   sonra reddetmek, bu görevin dört turdur kapattığı sınıfın ta kendisi.
> - **Kullanılmış satır asılı kalır ve ekran bunu KURAL gibi söyler, arıza gibi
>   değil** (§4.3): `transactions` satırları olduğu lokasyona işaret eder; silinebilen
>   bir lokasyon, kanıtı düzenlenebilen bir mesai kaydı demektir. Arşiv sütunu
>   olmadığı için "emekliye ayırma" da yok — bu seçenek (c)'ye kalır.
> - **Departmanlara da aynı kural** (`employees` + `transactions`, ikisi de RESTRICT).
>   Simetri kasıtlı: müdür iki listede iki farklı mantık öğrenmemeli.
> - **Sayım EKRANINDIR, FK GUARD'DIR.** Sayım sıfır dedikten sonra biri o lokasyona
>   çalışan atayabilir → `DELETE` **23503** verir, satır kalır. Bu **doğru sonuç**;
>   `ErrVenueInUse`'a çevrilir ve ekranda cümle olur, **500 değil**. Yarış gerçekten
>   tetiklenerek test edildi (`TestVenues_TheRaceBetweenCountingAndDeletingIsRefusedNotCrashed`).
> - **Onay adımı YENİDEN KULLANILDI, ikinci kalıp icat EDİLMEDİ** — ve bu, **sevk
>   edilmiş koda dokunmayı** gerektirdi; ayrıntı aşağıdaki tarihli blokta.
>   `deactivateconfirm.go`'nun jetonu HMAC imzalı, TTL'li ve **artık ÜÇ** şeye bağlı:
>   **eylem + özne + panel oturumu**. İçinde çalışana özgü bir şey yok, yani lokasyon
>   id'si özne olarak birebir oturuyor. (Bir kalıbı yarım kopyalamak bu repoda üç kez
>   oldu; dördüncüyü önlemenin en ucuz yolu aynı kodu çağırmak.) Silme,
>   deaktivasyondan **daha** geri alınamaz olduğu için onay burada daha da gerekli.
> - **Maliyet ölçüldü ve kontrolün YERİNİ o belirledi** (2026-08-08, `EXPLAIN (ANALYZE,
>   BUFFERS)`, dev DB'nin en büyük tenant'ı — 9 lokasyon):
>
>   | şekil | üç koşu (ms) |
>   |---|---|
>   | dört `count(*)`, listenin **her satırında** | 249,5 / 195,0 / 197,7 |
>   | dört `count(*)`, **tek** lokasyon (düzenleme kartı — **sevk edilen**) | 31,3 / 29,9 / 27,0 |
>   | **hiçbir şey** — liste kontrol sunmuyor | **0** |
>
>   Süreyi yiyen `transactions` yüklemi: `transactions(tenant_id, location_id)`
>   üzerinde indeks **yok**, bitmap heap scan oluyor. Kontrol bu yüzden **düzenleme
>   kartında**: liste **hiç** ödemiyor, ~30 ms yalnız müdürün açtığı lokasyon için.
>
>   🔴 **ELENEN alternatifin — satır başına dört `EXISTS` — TEK BİR SAYISI YOK, ve
>   bunu öğrenmek İKİ yanlış sayıya mal oldu.** `OR` **kısa devre yapar** ve pahalı
>   olan `transactions` yüklemi, yani maliyet **verinin** özelliği:
>
>   | popülasyon | üç koşu (ms) |
>   |---|---|
>   | yazılı sıra, **27 987 işlemli** tenant (9 lokasyon) | 8,4 / 6,4 / 7,3 |
>   | aynı tenant, `transactions` yüklemi **başta** | 241,8 / 150,7 / 143,0 |
>   | yazılı sıra, **0 işlemli** tenant (8 lokasyon, hepsi referanssız) | 0,55 / 1,26 / 0,34 |
>
>   Aynı ifade, **üç büyüklük mertebesi**. İlk satır ucuz çünkü **ikinci** yüklem
>   (`employees`) 9 satırın hepsinde `TRUE` — `tags` ve `transactions` **hiç
>   çalışmıyor**. Son satır ucuz çünkü o tenant'ın **hiç işlemi yok**. `count(*)`
>   veriye bakmadan aynı maliyeti veriyor; seçimin gerekçesi bu.
>
>   ⚠️ **Bu bloğun İKİ önceki sürümü de yanlıştı ve ikisi de ETİKETLEME hatasıydı,
>   ölçüm hatası değil.** İlki *"dört `EXISTS` … 8,154 ms"* diyordu — kısa devre
>   vakasının gerçek sayısı, şeklin maliyeti gibi sunulmuş. İkincisi bunu *"258,9 /
>   194,1 / 188,1 ms, **referanssız** satırlarda"* diye "düzeltti" — o da gerçek bir
>   ölçümdü ama **tenant filtresi OLMAYAN**, 122 000 satırlık tablonun tamamını tarayan
>   bir sorgunundu, ve cümle referanssız sayısı **SIFIR** olan bir tenant'ı
>   adlandırıyordu. İkisinde de **sayı doğru, popülasyon uydurma**. Bundan sonraki
>   kural: bir süre, **tenant'ı + satır sayısı + işlem sayısıyla** yazılır, ya da hiç
>   yazılmaz.
>
>   💡 **Seçenek (c) hâlâ açık ve şimdi bir maliyeti var:** `transactions(tenant_id,
>   location_id)` indeksi 288 ms'i düşürürdü ve arşiv sütunuyla birlikte **Faz B'nin
>   00013'üne** doğal olarak sığar.
>
> ⚠️ Ayrıca **`locations`'da `updated_at` YOK** (00002 yalnız `created_at` taşır).
> Bu yüzden Faz A'nın yazmaları `audit_log`'a **aynı transaction'da**
> (`audit.RecordTx`) kaydedilir: `static_ips`/GPS/vardiya **kararı belirleyen**
> verilerdir (§5 satır 6–7) ve değişikliğin izi **başka hiçbir yerde kalmaz**.
> Eylem adları mevcut kalıba uyar ve **altı** tanedir: `location.created` ·
> `location.updated` · `location.deleted` · `department.created` ·
> `department.updated` · `department.deleted`. (Son ikisi aynı blokta karara bağlanan
> silme için; `deleted` satırının `detail`'i **satırın kendisini** taşır — ad, IP
> aralıkları, koordinat, vardiya — çünkü silmeden sonra bakılacak satır kalmıyor.)
>
> ⚠️ Ve **`locations.name`/`departments.name` sınırsız `text`** (UNIQUE de yok).
> Faz A sınırı **sınırda** zorluyor (`tenant.MaxVenueNameRunes = 80`, rune bazlı) ve
> testle pinliyor; **şemaya CHECK eklemek** doğru evi olur ve bu şemaya bir daha
> dokunulduğunda (Faz B'nin 00013'ü) yapılmalı.


> **📌 KAYDA GEÇTİ (2026-08-08, M6-06 A denetimi): FK'SİZ BİR BAĞ VAR — Faz B / M6-09
> yükümlülüğü.** `policy_attachments.resource` **serbest metin** ve `location/<id>` ·
> `department/<id>` taşıyabiliyor (00007 bunu bilinçli yazıyor: *"grammar is validated
> in the application, not by a DB CHECK"*); `internal/domain/tap/decide.go:663` istek
> kaynağını tam bu şekilde kuruyor. **Foreign key YOK**, yani böyle bir satır varken
> lokasyon silme onu **sessizce yetim bırakır** ve ekran yine *"Nothing belongs to this
> venue"* der — altı RESTRICT anahtarının hiçbiri bu bağı görmez.
>
> **Bugün üretilemiyor, ölçüldü (2026-08-08):** dev DB'de **501** adet
> `location/<uuid>` satırı var ama **hepsi** `internal/db/rls_test.go`'nun rastgele
> uuid'leri — **0'ı** gerçek bir lokasyona çözülüyor. Tek üretim yazıcısı
> (`policyset.go` → `EnsurePolicyAttachment`) yalnız baseline'ın `location/*` ve `*`
> kalıplarını yazıyor. Yani Faz A'nın silme kararı bugün güvenli.
>
> **Onu üretecek olan [M6-09](#m6-09--policy-yönetim-ekranı)** — "kapsam bağlama:
> politika tüm tenant'a mı, belirli lokasyona/departmana mı (`resource`)" kriteri tam
> olarak bu satırları yazacak. M6-09 sevk edilmeden **önce** şunlardan biri gerekir:
> (a) silme yolunda `policy_attachments`'ı da sayan bir kontrol, (b) `resource` için
> gerçek bir FK/CHECK (migration), ya da (c) kararın *"politika eki silmeyi
> engellemez, yetim ek zararsızdır"* diye **yazılı** olması. Üçü de bir §4 kararıdır,
> ajanınki değil.

> **Kart düzeltmesi (2026-08-08, M6-06 A — onay jetonu v2).** Faz A, **M6-05 B'de sevk
> edilmiş** üç dosyaya dokundu. agent-brief sabit kural 8 gereği açıkça işaretleniyor:
>
> | dosya | ne değişti |
> |---|---|
> | `internal/handler/deactivateconfirm.go` | payload **v1 → v2**; `mint`/`parse` imzalarına **`action string`** eklendi; üç eylem sabiti (`employee.deactivate` · `location.delete` · `department.delete`); parça listesi **11 → 12** |
> | `internal/handler/employeeactions.go` | `setDeactivateConfirmation`/`confirmationRefusal` artık eylem-alan `setConfirmation`/`confirmationRefusalFor`'un ince sarmalayıcısı; deaktivasyon davranışı **değişmedi** |
> | `internal/handler/employeeactions_test.go` | mevcut jeton testleri eylemi taşıyor (hepsi `employee.deactivate`) |
>
> **Neden Faz A'da:** silme kapısı **aynı ilkeli** kullanıyor. Bir denetim ölçtü ki
> eylem bağı yokken, **venue silme için mint edilen jeton `/admin/employees/deactivate`
> kapısını geçiyordu** — istek yalnız DOMAIN'de düşüyordu, çünkü bir lokasyon id'si bir
> çalışan id'si değil. Yani kapıyı tutan şey **iki kaza** (uuid uzaylarının çakışmaması
> ve domain'in kontrol etmesi) idi; jetonun kendisi bunu bilmiyordu. Şema değişikliği
> o kazayı sessizce kaldırabilir.
>
> **v2 ne bağlıyor:** `sürüm | damga | EYLEM | özne | oturum`, uzunluk önekli HMAC
> altında. Eylem **özneden ÖNCE** kontrol ediliyor ve reddi `errConfirmInvalid`
> (toplanmış) — ayrı bir cümle, sondalayana hangi bağı kaçırdığını söylerdi.
> **v1 payload'ı onarılmıyor, "malformed" sayılıyor**; bedeli deploy anında on dakikalık
> pencere içinde en fazla bir kez yeniden basmak.
>
> ⚠️ **KAPANMAYAN limit, sayılıyor:** jeton **hâlâ tek kullanımlık değil** (sunucu
> tarafı durum yok). Kapatmak bir "harcanmış değer" tablosu + saklama politikası ister;
> ayrı bir iş. Her kapının bunu neden taşıyabildiği yazılı: deaktivasyonda
> `status <> 'deactivated'`, silmede ikinci `DELETE` hiçbir satırla eşleşmiyor.

> **Kart düzeltmesi (2026-08-09, M6-06 A — silme onayı: OKUMA C′).** Silmeden sonra
> müdürün hiçbir şey görmemesi (Okuma A) kullanıcıya üç okumayla soruldu; **C′ seçildi.**
>
> **Şekil.** Silme yönlendirmesi `?done=venue-deleted&id=<kaldırılan id>` taşır. Bölüm,
> başlığı basmadan **önce ve aynı istekte**, `audit_log`'da şu satırı arar:
> `tenant_id` = oturumun tenant'ı · `actor_id` = **oturumun yöneticisi** · `action` =
> `location.deleted` (ya da `department.deleted`) · `target` = URL'deki id ·
> `at > now() - 30s`. Satır **varsa** başlık basılır ve **venue'nun adı O SATIRDAN**
> gelir; **yoksa hiçbir şey basılmaz — hata değil.** Yabancı/bayat URL sade listeyi
> görür. Bu, M6-05'in kuralı (2)'nin — *"handler onu aynı isteğin okuduğu satıra karşı
> doğrular"* — silmeye uygulanmış hâli: satır gitti, ama **audit satırı duruyor**.
>
> **Üç okumanın sayıları:**
>
> | okuma | maliyet | ne alır | neden seçilmedi / seçildi |
> |---|---|---|---|
> | **A — sessizlik** | 0 | hiçbir zaman yanlış cümle | müdür açık onay görmüyor |
> | **B — imzalı iddia** | 1 MAC | doğrulanabilir cümle | ⛔ **jeton tek kullanımlık DEĞİL** → kusuru **kapatmaz, taşır**; TTL içinde tekrar harcanabilir |
> | **C′ — audit destekli** | **0,101 / 0,097 / 0,204 ms** | yabancının basamayacağı cümle + **adı** | ✅ **seçildi** |
>
> Ölçüm (2026-08-09, kendi kuralımıza uygun: tenant + satır + işlem): tenant
> `10000000-…-0001`, **9 lokasyon**, **28 042 işlem**, `audit_log`'da bu tenant için
> **2 019** satır (tabloda toplam **45 670**). Plan: `audit_log_tenant_at_idx` üstünde
> **Index Scan**, `Buffers: shared hit=3`. **Migration YOK** — indeks zaten var.
>
> **Neden C′, düz C değil:** düz C aktörün **en son** silmesini ilan eder; iki sekmede
> iki silme yapan müdüre **yanlış** venue'yu söyleyebilir. C′ id'yi yönlendirmeye koyup
> **o satırı** doğrular → her sekme kendi işini ilan eder. Ölçülen fark: **yok**
> (0,08–0,10 ms vs 0,07–0,41 ms).
>
> **Sağlamlık kayıtları.**
> - **Oracle değil:** sorgu hem `tenant_id` hem `actor_id` kapsamlı. Başka tenant'ın
>   gerçek silinmiş id'si · **aynı tenant'ta başka bir yöneticinin** sildiği id · var
>   olmayan uuid · bozuk uuid → **dördü de aynı sayfa, aynı durum kodu** (testle
>   karşılaştırıldı, bayt bayt).
> - **Yetkilendirme değil:** bu cevapla hiçbir şeye izin verilmiyor, yalnız bir cümle
>   basılıyor. Bu yüzden **okuma patlarsa 500 yok** — başlık basılmaz, liste normal
>   render edilir.
> - **Sık yol hiçbir şey ödemiyor:** sorgu yalnız iki kelimeden biri **ve** okunabilir
>   bir id varken koşuyor. Testte **sorgu sayısı** sayılıyor, bayrak değil.
> - **Ad URL'den ASLA gelmiyor** — id yalnız arama anahtarı; cümle ve ad audit
>   satırından.
> - **`actor_id`/`target` cast'leri yük taşıyor:** ikisi de nullable (00005), cast
>   olmasa sqlc pointer üretiyor ve `nil` bir aktör **her SYSTEM olayını** eşleştirirdi.
>   (`CountDepartmentReferences`'ın `department_id` kusurunun aynısı.)
>
> ⚠️ **TEST EDİLMEMİŞ LİMİT — pencere.** 30 saniyenin **üst** ucu test edilmedi:
> 31 saniyelik bir satır kurulamıyor. `at` veritabanının `DEFAULT now()`'ı,
> `RecordAuditEvent` onu parametre almıyor, ve `audit_log` `tappa_app` için
> **append-only** (yalnız SELECT + INSERT; trigger `tappa_owner`'ı bile durduruyor).
> Alternatifler daha kötü: 31 sn uyumak her koşuya eklenir, GRANT'ı gevşetmek
> mekanizmanın dayandığı append-only güvencesini siler. **Test edilen** yön: pencere
> **sıfır** olduğunda taze bir satır bile bulunmuyor — yani sınır gerçekten uygulanıyor.
> Bu bir **limit, "kapalı" değil.**

> **Kart düzeltmesi (2026-08-09, M6-06 A — güvenlik denetimi sonrası).**
> `tappa-security-auditor` **ONAY** verdi; dört bloklamayan bulgunun biri **kullanıcı
> kararı** oldu.
>
> 🔴 **X1 — SİLME ARTIK `owner`-ONLY (kullanıcı kararı, 2026-08-09).** Rol sütunu
> M6-02'den beri var ve dolu (`00006:66`, dev DB'de **30 359 owner / 7 417 manager**,
> **2 814** tenant'ta ikisi de) ama panel yazma yollarında **hiçbir satır** okumuyordu.
> Boşluk eski; **bedeli** değişti: M6-05 geri alınabilir durumlar yazıyordu, bu faz
> ürünün **ilk geri alınamaz** yolunu açtı ve arşiv yok.
> - **İki ayrı yükümlülük:** ekranda kontrol **hiç çıkmıyor** + **nedeni yazıyor**
>   ("owner'a ayrılmış"); ve **sunucuda** `mayRemove` reddediyor. UI'yi atlayan POST
>   `?problem=not-permitted` alıyor. **Ayrı ayrı** test edildi.
> - **Jeton da kapsandı:** manager onay jetonunu **mint edemiyor** (kontrol
>   `removalView`'da, sayımdan bile önce) — yoksa kapı iki aşamalı olur, ilki açık kalır.
> - **Rol yeni okuma GEREKTİRMİYOR:** `adminauth.Resolved.Role` oturum çözümlemesinde
>   `admin_users` satırından geliyor → **ek sorgu 0**, ve istemci veremiyor.
> - **Kapsam yalnız SİLME.** Oluşturma/düzenleme/deaktivasyon değişmedi; manager'ın
>   `venue`/`department` kaydetmesi testle pinli (sessiz genişleme yasak).
>
> 🔵 **X1(3) — REDDEDİLEN DENEME: iki okuma, karar sizin.** Geçici olarak **yalnız
> yapılandırılmış log** uygulandı.
>
> | okuma | lehine | aleyhine |
> |---|---|---|
> | **audit_log satırı** (`location.delete_refused`) | Üründe **beş** emsali var: `activation.failed` 6 527 · `admin.login.failed` 2 868 · `tap.rate_limited` 1 177 · `admin.login.tenant_refused` 499 · `admin.login.rate_limited` 16. Müşterinin **kendi** denetçisine görünür, tenant kapsamlı. Şişme sınırlı: `adminSessionLimit` bir oturumu **300 istek / 10 dk** ile bağlıyor. | `audit_log` **append-only** — şekli yanlış çıkarsa geri alınamaz. |
> | **yalnız log (uygulanan)** | Kalıcı iz bırakmıyor, her an audit satırına **yükseltilebilir**. | Müşterinin denetçisine **görünmez**; süreç log'unun saklama hikâyesi yok. |
>
> Geçici seçimin ölçütü **geri alınabilirlik**: bugün yazılmayan bir satır yarın
> yazılabilir, bugün yazılan geri alınamaz.
>
> 🟠 **X2 — `00005:228` ile uzlaştırma.** O migration *"detail'a SIR (token/anahtar/tam
> GPS) YAZILMAZ"* diyor; bu faz `detail`'a altı ondalıklı **bina** koordinatı yazıyor
> (508 `location.created`'ın 128'i · 202 `updated`'ın 4'ü · 58 `deleted`'ın 23'ü).
> **Migration DEĞİŞTİRİLMEDİ** (§3); uzlaştırma değerin yazıldığı yere
> (`venue.go`) **`00005:228`'e açık atıfla** yazıldı: §4.2/§4.7'nin hedefi **kişi
> takibi** — tap'in GPS'i bir **insan** hakkındadır; bir lokasyonun koordinatı
> yapılandırmadır, **aynı tenant'ta, aynı RLS altında zaten duruyor**, yani yeni
> maruziyet yok. Süreç log'u hâlâ yalnız `has_gps` bool alıyor.
> - **ADR gerekir mi — ölçüm:** `audit.Event{}` kurulan **26** yer var (8 dosya:
>   `activate.go` 8 · `adminlogin.go` 7 · `venue.go` 4 · `staff.go` 2 · `checkin.go` 2 ·
>   `channel.go` · `ratelimit.go` · `review.go`). Bunlardan **koordinat taşıyan
>   yalnız `venue.go`** (3 alan: `gps`, `from_gps`, `to_gps`). Yani bu bir **sınır
>   değişikliği değil**, mevcut sınırın **ilk kez uygulanan yorumu** ve bugün
>   **tekrarlamıyor**. Tekrarlarsa (M6-08 manuel kayıt, M6-09 policy) ADR'ye döner.
>   **Karar sizin.**
>
> 🟡 **X3 — silme izi artık YOKLUĞU da kaydediyor.** `omitempty` yüzünden 58
> `location.deleted` satırının **37'sinde `static_ips` anahtarı hiç yoktu** — "IP
> kanıtı yoktu" ile "o sürüm kaydetmiyordu" ayırt edilemiyordu, ve silmeden sonra **bu
> satır tek hayatta kalan kayıt**. Artık `"static_ips": []` · `"gps": ""` · `"shift":
> ""` · `"wifi_ssid": ""` açıkça yazılıyor, ve **`created_at` eklendi** (iki `DELETE`
> de artık onu `RETURNING`'e alıyor) — `locations`'ta `updated_at` olmadığı için
> silinen satırın kayıt zamanı **başka hiçbir yerde** yoktu. `created`/`updated`
> izlerinde `omitempty` **korundu**, gerekçesi yazıldı: orada satır hâlâ okunabiliyor,
> izin işi **değişeni** kaydetmek; silmede satır yok, izin işi **ne olduğunu**
> kaydetmek.
>
> 🟡 **X4 — ekran artık saydığını söylüyor.** *"Nothing belongs to this venue"* →
> *"No departments, employees, plaques or records belong to this venue"*.
> `CountLocationReferences` altı FK'yi sayıyor; `policy_attachments.resource`
> **serbest metin** ve FK'siz. ⚠️ **Yükümlülük M6-09'dur, M7 değil** — ölçüldü:
> `m7-portal.md`'de policy/resource geçmiyor (1 alakasız eşleşme), M6-09'un kabul
> kriteri ise birebir bu (*"Kapsam bağlama: politika tüm tenant'a mı, belirli
> lokasyona/departmana mı (`resource`)"* — **[M6-09 — Policy yönetim ekranı](#m6-09--policy-yönetim-ekranı)**
> bölümünün kabul kriterleri arasında). Denetçinin M7 işareti yanlış, kartın M6-09'u
> doğru. ⚠️ **Bu cümle önce "kart satır 3608" diyordu ve kriter 3716'daydı** — satır
> numarası kayan bir iddia; bu kartta artık **bölüm başlığına** atıf yapılıyor.

> **Kart düzeltmesi (2026-08-09, M6-06 A — X1(3) kapandı + ADR eşiği).**
>
> ✅ **KULLANICI KARARI: reddedilen silme denemesi `audit_log`'a YAZILIYOR.** Bir önceki
> blokta geçici olarak *"yalnız yapılandırılmış log"* uygulanmıştı; karar audit
> satırından yana verildi. Belirleyici olan **müşterinin kendi denetleyicisine
> görünürlük**: bir owner, manager'ının tekrar tekrar lokasyon silmeye çalıştığını
> görebilmeli, ve süreç log'unun ne tenant sınırı ne saklama hikâyesi var. Şekil riski
> zaten düşüktü — **beş emsal** aynı kalıbı kuruyor.
> - **Adlar emsallerden türetildi, uydurulmadı:** `location.delete_refused` ·
>   `department.delete_refused`. `admin.login.tenant_refused`'ın `_refused` sonekini
>   alıyorlar, çünkü `location.deleted` zaten **başarılı** eylemin adı ve
>   `action LIKE 'location.%'` tarayan biri ikisini tek bakışta ayırabilmeli.
> - **`detail` X3'ün dersini uyguluyor:** `omitempty` yok, boş değerler açıkça yazılı.
>   Taşıdıkları: `outcome` · `reason` · `what` · `role` (aktörün gerçek rolü) ·
>   `required_role`. `actor_id` denemeyi yapan, `target` hedeflenen satır.
>   ⚠️ **§4.7: sır yok** — jeton, çerez, koordinat, oturum id'si hiçbiri; onay değeri
>   ret anında **okunmuyor** bile. ⚠️ **Ve bu artık İZİN VERİLEN ANAHTAR KÜMESİYLE
>   pinli, yasaklı dize taramasıyla değil** — bir denetim yedi kelimelik denylist'i tek
>   düzenlemede yendi (`Note` alanına oturum id'si + onay değeri kondu, saklanan jsonb
>   `"note":"session=… confirm="` oldu, **üç test de yeşil** kaldı; bir uuid o yedi
>   kelimenin hiçbirine çarpmıyor, base64 bir jeton da çarpmaz). Şimdi `detail`'in
>   anahtar kümesi **tam olarak** `outcome · reason · what · role · required_role`
>   olmalı; yeni bir alan, birisi onu listeye **gerekçesiyle** ekleyene kadar testi
>   kırar.
> - **Kendi transaction'ı** (`audit.Record`, `RecordTx` değil): silme **olmuyor**, yani
>   kaderini paylaşacağı bir yazma yok. `audit.Record`'un kendi yorumunun tarif ettiği
>   vaka bu — *"bu pakete en çok ihtiyacı olan çağıran, ana transaction'ı BAŞARISIZ
>   olandır"*.
> - **Audit yazımı patlarsa müdür ret cümlesini görür, 500 GÖRMEZ.** Bu bir **kaydetme**
>   yolu, **yetkilendirme** yolu değil: ret zaten bir satır önce gerçekleşti, ve bizim
>   iz arızamızı onların isteği hakkında bir hüküm gibi sunmak yanlış olurdu. Beş
>   emsalin hepsi `a.record`'un aynı davranışına dayanıyor (log'la, devam et).
> - **Log satırı DA korundu — iki temsil değil, iki OKUYUCU.** `adminlogin.go`
>   `ActionAdminLoginRefused`'da aynısını yapıyor: log operatöre anlık ve greplenebilir
>   bağlam verir (izde sütunu olmayan), audit satırı tenant'ın owner'ına kalıcı ve
>   kapsamlı kayıt verir. Birini atmak bir okuyucuyu sessizce silmek olurdu.
> - **Şişme sınırı ölçülü:** `adminSessionLimit` bir oturumu **300 istek / 10 dk** ile
>   bağlıyor.
>
> 📌 **ADR KARARI: GEREKMİYOR (orkestratör, 2026-08-09).** `audit_log.detail`'a
> koordinat yazan yer sayısı **bugün 1/27** — `audit.Event{}` **27** çağrı yeri / **9**
> dosya (`activate.go` 8 · `adminlogin.go` 7 · `venue.go` 4 · `staff.go` 2 ·
> `checkin.go` 2 · `channel.go` · `ratelimit.go` · `locationactions.go` · `review.go`);
> koordinat taşıyan **yalnız `venue.go`**, 3 alan (`gps`, `from_gps`, `to_gps`).
> CLAUDE.md §10 ADR'yi *"güvenlik sınırı **değiştiyse**"* istiyor; bu bir sınır
> değişikliği değil, mevcut sınırın **ilk uygulaması** ve tekrarlamıyor.
> `venue.go`'daki `00005:228` uzlaştırması + bu kart yeterli.
> - ⚠️ **DÜZELTME: bu blok önce "26 / 8 dosya" diyordu ve KENDİ EKLEDİĞİ yazıcıyı
>   saymıyordu** — dokuzuncu dosya, bu bloğun kararlaştırdığı ret satırını yazan
>   `locationactions.go`'nun ta kendisi. Yük taşıyan iddia (koordinat yazan tek yer
>   `venue.go`) değişmedi, ama eşiği sayacak bir sonraki okuyucu yeni yazıcıyı
>   **ikinci koordinat yazıcısı** sanabilirdi.
> - ⚠️ **EŞİK, bir sonraki kişi görsün diye:** **ikinci bir yer** `audit_log.detail`'a
>   koordinat yazmaya başlarsa (yani oran **2/27** olursa) bu bir **ADR'yi hak eder** —
>   çünkü o noktada bu artık tek bir yorum değil, bir **kalıp** olur. En olası
>   adaylar: **M6-08** (manuel kayıt) ve **M6-09** (policy `resource` kapsamı).

> **Kart düzeltmesi (2026-08-09, M6-06 B veri katmanı sırasında).** Migration
> **00013** sevk edildi (`00013_harden_tags_and_add_inventory.sql`). Üç yazılı
> iddia gerçekle çelişti; üçü de ölçümle düzeltildi.
>
> 🔴 **(1) T8'in ÖNCÜLÜ YANLIŞTI — "mevcut satırlar zaten büyük harf, veri
> taşıması yok" bu veritabanı için doğru değil.** Ölçüm (2026-08-09): **75 438**
> tag satırının **57 428**'i büyük harf, **18 010**'u **küçük harf**. Doğrulanmış
> (validated) bir CHECK bu yüzden **eklenemiyor** — `ALTER TABLE … ADD CONSTRAINT`
> canlı olarak `is violated by some row` veriyor.
> - **Ve normalleştirme bir KIRMIZI ÇİZGİYE çarpıyor, tercihe değil.** O 18 010
>   satırın **12 437**'si `transactions`'ın **24 874** satırından
>   `transactions_tag_fk` ile referanslanıyor (MATCH SIMPLE / NO ACTION), ve
>   `transactions` veritabanı düzeyinde append-only (00005'in tetikleyicisi
>   `tappa_owner`'ı da bağlıyor). "Hepsini büyük harfe çevir" = tetikleyiciyi
>   superuser olarak devre dışı bırakıp **24 874 delil satırını yeniden yazmak**
>   (§4.3). Yapılmadı.
> - **Sevk edilen şekil: `NOT VALID`.** Yeni her INSERT ve UPDATE denetleniyor
>   (ölçüldü: küçük harfli INSERT **reddediliyor**); artık satırlar duruyor ve
>   **donuyor** (o satırlara yapılan HERHANGİ bir UPDATE de reddediliyor —
>   fail-closed, ölçüldü). Taze bir veritabanında sıfır satır ihlal ettiği için
>   üretimde bu kısıt fiilen doğrulanmış doğar; tamamlayan tek satır
>   `ALTER TABLE tags VALIDATE CONSTRAINT tags_uid_canonical_hex;`.
> - **18 010'un kaynağı ölçüldü ve KAPATILDI:** ikisi de `\xDEAD` placeholder
>   yazan **iki test yardımcısı** (`internal/db/store_test.go` ve
>   `internal/sun/advance_test.go`) `hex.EncodeToString`'i `ToUpper`'sız
>   kullanıyordu. Handler tarafındaki ikizleri zaten `ToUpper` sarıyordu. İkisi de
>   düzeltildi → artık satır **birikmiyor**.
>
> 🔴 **(2) T9'un ÖNERDİĞİ TRIGGER KOŞULU REPLACE TAG AKIŞINI KİLİTLİYOR.** Backlog
> `NEW.last_ctr > OLD.last_ctr` yazıyor. **Gereklilik olarak yanlış** — canlı
> ölçüm: o koşulla, `last_ctr`'a dokunmayan bir retire
> (`status/retired_at/replaced_by`) **reddediliyor**:
> `read counter is monotonic … (0 -> 0 refused)`. Sevk edilen koşul geriye gitmeyi
> yasaklar, ilerlememeyi değil: **`WHEN (NEW.last_ctr < OLD.last_ctr)`**. Aynı
> retire o koşulla **UPDATE 1**. İkisi de testle pinli, ve yanlış koşul mutasyon
> olarak koşulup **KIRMIZI** görüldü.
>
> 🔴 **(3) T9'un SÜTUN LİSTESİ EKSİKTİ — envanter modeliyle çelişiyordu.** T9
> `(last_ctr, status, retired_at, replaced_by)` diyor; envanter modeli ise panelin
> plaketi bir lokasyona **bağlamasını** istiyor ve o bağlama `location_id`'nin
> UPDATE'idir. İkisi aynı günün kullanıcı kararları, yani biri diğerini
> uygulanamaz kılıyordu. Sevk edilen grant: **`(location_id, last_ctr, status,
> retired_at, replaced_by)`**. Yazılmayan dört sütun: `uid` · `tenant_id` ·
> `aes_key_ref` · `created_at`. Çapraz-tenant taşıma bileşik FK ile yapısal olarak
> zaten imkânsız.
> - **Beklenmedik kazanç:** `tenant_id` artık **yetki** düzeyinde reddediliyor
>   (`permission denied`), eskiden yalnız RLS `WITH CHECK` reddediyordu.
> - **§4.4 REGRESYON YOK, ölçüldü:** `AdvanceTagCounter`'ın `FOR UPDATE` satır
>   kilidi **sütun-düzeyi** grant altında çalışıyor (bariz değildi, sondalandı) ve
>   replay hâlâ `pgx.ErrNoRows` — tetikleyici o ifadeden **erişilemez**, çünkü katı
>   `prev.old_ctr < @ctr` zaten 0 satır eşleştiriyor.
>
> ✅ **ENVANTER MODELİ ŞEMAYA GİRDİ ve kartın `encoded/pending` kriteri artık
> temsil edilebilir.** `location_id` **nullable**; `status` kümesi
> **`unassigned`** kazandı; iki CHECK ikisini birbirine bağlıyor
> (`tags_active_requires_location` · `tags_unassigned_has_no_location`), böylece
> "hizmette mi" sorusunun iki çelişkili temsili olamıyor. **pending** =
> `location_id IS NULL`, **encoded** = satırın kendisi (Tappa yüklemeden önce
> sarmalıyor). `tenant_id` **NOT NULL kaldı** (§4.5) — plaket bir müşteriye sevk
> edilir, tenant yükleme anında bellidir.
>
> 🔴 **VE BU DEĞERİN POLİTİKA MOTORUNDA BİR FAIL-OPEN'I VAR — B'nin uygulama
> turuna DEVREDİLDİ.** `sys:tag-not-active` bir **denylist**:
> `s == "retired" || s == "lost"`. `active` **istemiyor**. Yani `unassigned` bir
> plakete tap §5 satır 1'e **hiç çarpmıyor**, 2–7'ye düşüyor ve `ok` olabiliyor.
> Düzeltme tek satır (`s != "active"`) ve bugünün sözlüğünde **kanıtlanabilir
> şekilde eşdeğer**. **Bugün erişilebilir değil:** `db/queries`'te `tags` üzerinde
> **INSERT yok** (panel plaket yaratmıyor — Tappa yüklüyor), yani `unassigned` bir
> satırı bugün yalnız `tappa_owner` üretebilir. T8'in "bugün sömürülemiyor"uyla
> **aynı şekil**, ve o koruma bir sonraki turda sona eriyor.
>
> 📊 **T11 İNDEKSİ SEVK EDİLDİ** (`transactions_tenant_location_idx`). Ölçüm
> (tenant `1000…0001`, 9 lokasyon, **29 087** işlem / tabloda **238 922**, 228 MB):
> dört sayım **30,3 / 29,8 ms → 8,9 / 6,9 ms**; yalnız `transactions` bacağı
> **20,5 / 18,3 ms → 0,069 / 0,045 ms** (Bitmap Heap Scan → **Index Only Scan**).
> Bedeli **4 472 kB** ve **269 ms** kurulum; yazma bedeli 20 000 INSERT'lik üç
> eşleştirilmiş örnekte **+0,8% / +0,1% / +6,8%** — yani bu makinede **ölçülebilir
> bir yavaşlama yok**, tek bir sayı yazmak dürüst olmazdı. ⚠️ **Kalan ~7 ms artık
> `employees` bacağı** (indekssiz) — ayrı bir iş, bilerek paketlenmedi. ⚠️ Bu,
> 00013'ün **tek performans** parçası: hiçbir test onsuz kırılmıyor, yani onu
> silen bir mutasyon CI'ya **görünmez** (00011'in `admin_users_email_idx` için
> yazdığı aynı sınır). Listeye kontrolü geri taşımak artık **uygulanabilir**, ama
> bu bir ekran kararı ve **verilmedi**.
>
> ⛔ **BU MADDE GERİ ALINDI — aşağıdaki 2. tur bloğuna (V2) bakın.** Tarayıcı
> `scripts/redline-check.sh` **HEAD'in hâlinde**; muafiyet **yok**. Cümle tarihli
> blok geleneği gereği silinmiyor, ama tek başına okunursa **yanlış** bilgi verir.
>
> ⚠️ **`make audit` KIRILDI VE TARAYICI DEĞİŞTİRİLEREK ONARILDI — bilinçli, dar,
> ve pozitif kontrollü.** Yeni testler sayacı **geriye saran** kosulsuz UPDATE'ler
> içeriyor (tetikleyicinin çalıştığını kanıtlamanın tek yolu) ve R4 bunları FAIL
> veriyordu. `_test.go` muafiyeti eklendi — **R3 ve R5'in aynı gerekçeyle zaten
> taşıdığı** kalıp. `db/` **tam taranmaya devam ediyor**: pozitif kontrol olarak
> üretim SQL'ine kosulsuz bir sayaç yazımı kondu → R4 **yine FAIL** verdi (exit 1),
> geri alınınca exit 0. Muafiyet genişletilmedi.
>
> 📌 **`transactions` üzerinde geriye dönük etki YOK** (T11 indeksi hariç, o da
> yalnız okuma planı): 00013 `transactions`'ın ne satırlarına ne şemasına dokunuyor.
> **ADR gerekmiyor** okuması: bu bir güvenlik sınırının *değişmesi* değil, iki
> mevcut sınırın (§4.4 atomik sayaç, §4.5 tenant izolasyonu) **daraltılması** —
> hiçbir yetenek eklenmiyor, dört sütunluk yazma yetkisi ve bir geri sarma
> yeteneği **kaldırılıyor**. Karar orkestratörün.

> **Kart düzeltmesi (2026-08-09, M6-06 B veri katmanı — 2. tur, üçüncü göz RED
> sonrası).** Üç bloklayan kapandı; ikisi bir öncekinde **yanlış yazılmış**
> iddialardı.
>
> 🔴 **(V1) KUŞAK DÖRT SORGUDA DA KANITSIZDI VE ONU KANITLADIĞINI SÖYLEYEN TEST
> YANLIŞTI.** Test sorguları **B'nin bağlamından** koşuyordu; orada RLS zaten 0
> satır veriyor, yani açık `tenant_id` yükleminin sonuca **hiçbir katkısı yoktu**.
> Denetçi dördünün de yüklemini `$n::uuid IS NOT NULL` ile değiştirdi → paket
> **yeşil** kaldı. Yeni test **A'nın bağlamında** koşuyor ve parametre olarak
> **B'nin tenant id'sini** veriyor: satır görünür, rol yazabilir, dolayısıyla
> ifadeyi reddedebilecek **tek şey kendi yüklemi**. Aynı mutasyon artık **dört alt
> testin dördünü de KIRMIZI** yapıyor (mutasyon derlendi, `diff -u` ile kanıtlandı).
> - **Ağ neden görmüyordu — ölçüldü, ve cevap sorgu şekli değil TARAYICI KAPSAMI:**
>   `storeQueryNames` (a) yalnız **kendi paket dizinini** ayrıştırıyor ve (b)
>   `_test.go` dosyalarını **eliyor**. Dört sorgunun tek çağıranı
>   `internal/db/tagsinventory_test.go` — yani `internal/db`'ye **dördüncü bir AST
>   kopyası** koysaydım **sıfır** isim türetip yeşil bir boşluk basacaktı.
> - **Çözüm: dosyadan türeten bir net**, `internal/domain/tenant/query_test.go`
>   içinde, **mevcut ve sertleştirilmiş matcher'ı** kullanarak (dördüncü tarayıcı
>   değil, ikinci **türetim**; ~40 satır). Bir sorgu **var olduğu için** denetleniyor,
>   **çağrıldığı için** değil — kapanan pencere tam olarak "SQL merge edildi, henüz
>   handler yok" aralığı. **Koşuda basılan sayı: `6 of 65` (dosya-türetimli), bu
>   paketin çağrı-türetimli 18'iyle birlikte `24 of 65 (%36,9)`.**
> - ⚠️ **Ve net kurulur kurulmaz gerçek bir kör nokta buldu:** matcher'ın `stopRE`'si
>   `FOR UPDATE`/`FOR SHARE` içermiyordu, yani **kilitli bir okuma**nın son koşulu
>   `p.tenant_id = @tenant_id\n FOR UPDATE` olarak okunuyor ve **doğru kapsanmış**
>   `AdvanceTagCounter` işaretleniyordu. Üç kopyaya da aynı üç token eklendi
>   (diğer ikisi için **no-op** — kapsadıkları hiçbir sorgu satır kilidi almıyor,
>   grep'lendi; ölçüm: ledger 9/64 ve review sayıları **değişmedi**).
> - ⚠️ **Bonus bulgu, düzeltilmedi, YAZILDI:** üç kopyanın "davranışsal olarak
>   birebir" olduğu iddiası **yanlış** — `ledger`'ın `stopRE`'sinde `RETURNING`
>   **yok**, diğer ikisinde var. Kapatmak o paketin belt'inin kabul ettiği kümeyi
>   değiştirir; kendi mutasyon turuyla gelmeli.
>
> 🔴 **(V2) R4 MUAFİYETİ GERİ ALINDI.** `scripts/redline-check.sh` artık **HEAD'in
> hâli** (`git diff` boş). Üç test ifadesi reponun kendi çok satırlı SQL üslubuna
> çevrildi ve `make audit` **dokunulmamış tarayıcıyla exit 0**. ⚠️ Bir kez daha
> **kendi yorumum** R4'ü kırdı (yasaklı şekli **alıntılayan** cümle) — M6-06 A'nın
> R2'de yaşadığının aynısı, ve çözümü yine **metni yeniden yazmak** oldu.
> - 🔴 **R4'ün gerçek açığı SATIR-YERELLİK ve artık YAZILI** (`tagsinventory_test.go`
>   başlığı). Ölçüldü, **üretim** konumlarında: tek satır → `rc=1 FAIL`; **aynı ifade
>   satırlara bölünmüş → `rc=0 SESSİZ`**, hem `db/queries` hem Go const'unda. Üçüncü
>   şekil **`AdvanceTagCounter`'ın kendi biçimi**.
> - **Çok satırlı R4 önerisi: ÖNERİLMİYOR, ve gerekçesi bir sayı.** `rg -U` ile
>   ölçüldü: **11 blok / 3 dosya**, **0 gerçek ihlal**. Yanlış alarmların içinde
>   `AdvanceTagCounter`'ın **kendisi** var — çünkü bugünkü muafiyet süzgeci
>   `last_ctr <` arıyor ve o ifadenin koruması CTE'de **`prev.old_ctr <`** diye
>   yazılı. Yani çok satırlı R4, **ürünün en kritik ifadesini kalıcı yanlış alarma**
>   çevirir; süzgeci de genişletmek ağa özel-durum biriktirmektir (M6-05 A'da bir tel
>   genişletmesi **30 meşru ölçümü** işaretleyip geri alınmıştı). **Karar sizin.**
>
> 🔴 **(V3) DEVRETTİĞİM 2. YÜKÜMLÜLÜK YANLIŞTI.** *"pointer değil → scan hatası →
> 500 → §4.6 kaybı"* — **öyle değil.** Bağımsız olarak yeniden ölçüldü:
> `SELECT NULL::uuid` → `uuid.UUID`'ye **hatasız** `uuid.Nil` olarak taşınıyor
> (kontrol: `[16]byte` **hata veriyor**). Gerçek zincir, her halkası kodda
> doğrulanmış: `uuid.Nil` → `GetLocationForTap` **ErrNoRows** →
> `locationResolved=false` (ele alınan dal) → `p.LocationID` **nil** →
> `transactions.location_id` nullable → **satır YAZILIR**, IP/GPS kanıtı yok →
> **`flag`**. Yani 500 yok, §4.6 kaybı yok — **ama §5 satır 1 hiç çalışmıyor ve stok
> plakete yapılan tap KAYIT ÜRETİYOR**. Bu, 1. yükümlülüğün (denylist) **tek gerçek
> savunma** olduğunu doğruluyor; pointer düzeltmesi bu deliği **kapatmaz**.
>
> 📊 **B3 — "son görülme" kriteri artık KARŞILANIYOR, ve indeks EKLENMEDİ.** Karar
> kuralı sayılardan **önce** kondu (T11'in profiline yakınsa ekle):
>
> | şekil | ölçüm (tenant `1000…0001`, 11 plaket, 24 585 taplı satır / 242 642) |
> |---|---|
> | plaket başına correlated subquery, indekssiz | 203,3 / 195,1 / 196,4 ms (Seq Scan) |
> | **tek `GROUP BY` geçişi, indekssiz — SEVK EDİLEN** | **30,9 / 26,6 / 23,2 ms** |
> | aynı sorgu + `(tenant_id, tag_uid, occurred_at DESC)` | 7,7 / 7,6 ms (Index Only, **12 MB**) |
>
> **Sorgu şekli indeksten daha çok kazandırdı** (195 → 26 ms, bedava). İndeksin
> kalan katkısı 26 → 7,7 ms için **12 MB** — T11'in profili **4 472 kB / ~400×**
> iken bu **2,7× boyut / 3-4× hız**, yani **yakın değil** → eklenmedi, gerekçe ve
> before/after sayıları migration'a yazıldı.
> - 🔴 **VE last_seen LİSTE SÜTUNU OLARAK KONULAMADI — TİP DÜRÜSTLÜĞÜ yüzünden.**
>   sqlc v1.28 dört şeklin dördünde de yalan söylüyor: `interface{}` (tipsiz) ya da
>   cast'le **NON-POINTER `time.Time`** — ki o da derleniyor ve **tam da bu görevin
>   eklediği satırlarda** çöküyor: `cannot scan NULL into *time.Time` (kendi pozitif
>   kontrolüm yakaladı). **Sıfır `time.Time`'ı "hiç taplanmadı" saymak, bu görevin
>   zaten devrettiği `uuid.Nil` hatasının ikizi olurdu.** Bu yüzden yokluk
>   **YOKLUKLA** temsil ediliyor: `ListTagLastSeen` yalnız **taplanmış** plaket için
>   satır döndürür, `GROUP BY` sayesinde `time.Time` **gerçekten** NOT NULL, ve
>   "hiç" bilgisi anahtarın **bulunmamasından** okunur. Testle pinli.
>
> 🟠 **B1 — `Down`'ın TARİFİ yanlıştı** (`Down`'ın kendisi doğru). *"bind or
> **retire** it"* deniyordu; izole DB'de ölçüldü: retire **yetmiyor**
> (`SET NOT NULL` → **23502**), yalnız **bağlamak** (ya da silmek) işe yarıyor —
> çünkü iki engel farklı: status CHECK `'unassigned'` kelimesine, `SET NOT NULL` boş
> lokasyona itiraz ediyor. Metin düzeltildi; ayrıca **başarısız `Down`'ın atomik**
> olduğu (goose tx'e sarıyor → 7 CHECK, trigger, indeks yerinde, v13) yazıldı.
>
> 🟠 **B2 — 1. turdaki mutasyonumdan ÜÇ SATIR KALINTI kalmıştı ve iddia 1'i
> çürütüyordu.** `EF6A9A125C36AB` / `…Ab` / `ef6a…` — aynı tenant, aynı lokasyon,
> `created_at` **15:40:40** (migration'dan **önce**), yani T8 testi **M1 mutasyonu
> altındayken** koşup üçünü de bırakmış: **T8'in tam senaryosu dev DB'de maddi
> olarak vardı**. 1. turda M5/M6'nın 3 satırını bildirmiş, bunları **bildirmemiştim**.
> Silindi (0 transaction, 0 `replaced_by` referansı); `upper(uid)` çakışması artık
> **0**. **Ders yazıldı: bir CHECK mutasyonu paylaşılan DB'de yıkıcıdır — sondalar
> `BEGIN … ROLLBACK` ya da ayrı DB olmalı.**
>
> 🟡 **B4 — `::char(14)` SESSİZCE KIRPIYOR ve bu artık SQL'de kapalı.** Ölçüldü:
> `'AABBCCDDEEFF01ZZZZZZ'::char(14)` → `AABBCCDDEEFF01`, **hata yok** — yani fazla
> uzun bir successor uid **başka, muhtemelen GERÇEK** bir plakete dönüşüyor ve
> self-FK onu kabul ediyor; audit zinciri **yanlış** halefe işaret eder ve kimse
> söylemez. `RetireTagForReplacement`'a **kırpılmamış** parametre üzerinde
> (`@replaced_by::text`) kanonik biçim yüklemi eklendi → `pgx.ErrNoRows`. Handler
> sınırı doğrulaması **hâlâ borç** (§7), ama unutmak artık sessiz değil.
>
> 🟡 **B5** — `internal/sun/params.go`'nun *"tags.uid CHECK allows both cases"*
> parantezi bayatlamıştı; **silindi** (bağımlılığı ters kuruyordu) ve yerine URL'nin
> neden hoşgörülü, **veritabanının neden katı** olduğu yazıldı.
>
> 🟡 **B6 — yeni SESSİZ-SKIP yolu kapatıldı.** `ownerPoolForTest` artık ikisini
> ayırıyor: **hiç DB yoksa** SKIP, **`DATABASE_URL` var ama migrate URL yoksa
> FATAL** — yarı yapılandırılmış bir makinede susturulacak şey bir **§4.7 iddiası**
> (tappa_app `aes_key_ref`'i yazamaz). `-short` skip sayısı **3'te kaldı**.
>
> **2198 PASS / 0 FAIL / 0 SKIP · 16 paket** · `fmt`/`gen`/`lint`/`audit` exit 0 ·
> migration **00013** uygulanmış, `Down` ayrı DB'de 13→12 ve tekrar 13.

> **Kart düzeltmesi (2026-08-09, M6-06 B veri katmanı — 4. tur, ikinci RED sonrası).**
> İki bloklayan da **kendi yazdığım cümlelerdi**; ikisi de ölçümle kapandı.
>
> 🔴 **(W1) SEVK EDİLEN KAYNAKTA VAR OLMAYAN BİR İNDEKSE MALİYET İDDİASI.**
> `ListTagLastSeen`'in yorumu *"one Index Only Scan over
> `transactions_tenant_tag_occurred_idx` (00013) — see the index for the numbers"*
> diyordu; o indeks **yok** (`pg_class` → 0), **aynı migration** onu *"MEASURED AND
> DELIBERATELY NOT ADDED"* diye yazıyor, ve gönderme indeksin **reddedildiğini**
> anlatan paragrafa çıkıyordu. İddia, indeksi **ekleyen** bir taslakta yazılmış ve
> karar dönünce **yeniden ölçülmemişti** — üstelik `internal/store/tags.sql.go`
> aynasına da sevk edilmişti. **Kendi ölçümüm** (tenant `1000…0001`, **11 plaket**,
> **29 575** işlem / **24 723**'ü tag'li, **244 512** satır, 238 MB):
> **HashAggregate → Bitmap Heap Scan on transactions**, sürücü **Bitmap Index Scan
> on `transactions_tenant_location_idx`** (00013'ün T11 indeksi), **5 047 buffer**,
> **3 çıktı satırı**, **21,2 / 24,7 / 26,1 ms**. Yorum buna eşitlendi ve `make gen`
> ile ayna güncellendi. **Bağımlılık da ölçüldü:** T11 indeksi düşürülünce planner
> `transactions_tenant_occurred_idx`'e düşüyor, **69 575** indeks satırı okuyor ve
> süre **47,7 ms** — yani T11 bu sorguya da **tenant filtresi** olarak yarıyor.
>
> 🔴 **(W2) YAKALAYAMADIĞI MUTASYONU ADLANDIRAN CÜMLE.** `TagUid == nil` iddiası
> *"IS NOT NULL yüklemi gitti"* diyordu; denetçi yüklemi sildi, **derledi**, koştu →
> **PASS**. Sebep: fixture taze bir tenant kuruyor ve **`channel='manual'`
> (tag_uid NULL) satırı hiç yok**, yani NULL grubu **oluşamıyor**. Fixture'a manual
> satır eklendi; aynı mutasyon artık **KIRMIZI**.
> - ⚠️ **Ve aynı sınıfı kendi dosyamda taradım — İKİ boşluk daha buldum ve ikisini
>   de koştum:** (a) *"katı guard ... UPDATE denenmeden reddeder"* → `prev.old_ctr <`
>   → `<=` (§4.4 replay deliğinin ta kendisi) **KIRMIZI**; (b) *"want SQLSTATE
>   23503"* → bileşik FK **düşürüldü** → **KIRMIZI**. İkincisi DDL olduğu için
>   **ayrı bir sonda DB'sinde** koştu (M5/M6 dersi) — çalışma DB'sine dokunulmadı.
>
> 🔴 **(N1) `make test` GÜVENİLMEZDİ VE BU BENİM DEĞİŞİKLİĞİM DEĞİLDİ — düzeltildi
> (onaylı kapsam genişletmesi).** `vat_number` UNIQUE, ve **21 test çağrı yeri**
> onu uuid'nin **ilk 8 hex hanesinden** üretiyordu (32 bit). Dev DB o uzayda
> **128 553** değer tutuyor → tek insert için çakışma **2,99e-5**. **Asıl sayı koşum
> başına:** bir `make test` **379 tenant** yazıyor (tabloyu önce/sonra sayarak
> ölçüldü) → **%1,13, yani ~89 koşumda bir**, ve payda hiç küçülmediği için
> **artıyor**. Tam uuid v4 (122 rastgele bit) ile aynı sayı **9,2e-30**. 19 dosyada
> 21 çağrı yeri düzeltildi (kalan: **0**), `make test` **iki kez** koşuldu, ikisi de
> **2200 PASS / 0 FAIL / 0 SKIP / 16 paket**.
>
> 🟠 **(N2) İKİ AĞ AYNI KÜMEYİ FARKLI SAYIYORDU.** Çalışma-zamanı belt testi
> *"all four"*, statik net *"all five"* diyordu; gerçekte kapsanmayan
> `ListTagLastSeen`'di — yüklemini bozunca `internal/db` **yeşil** kalıyordu.
> Beşinci alt test eklendi (**mutasyon KIRMIZI**), iki başlık aynı kümeyi
> adlandırıyor, ve statik netin **sayı yazan** cümlesi kaldırıldı (küme türetiliyor;
> o cümle aynı görev içinde bir kez daha bayatlamıştı).
>
> 🟡 **(N3)** Matcher **fail-closed** bir yanlış alarm veriyor: tümü parantezli bir
> `WHERE (a AND b)` maskelenip *"has NO WHERE clause at all"* oluyor. Delik değil
> (doğruyu reddediyor, yanlışı kabul etmiyor) — **limit olarak yazıldı**.
> 🟡 **(N4)** `ledger`'ın eksik `RETURNING`'i **iki yönde de ölçüldü**: tenant bağını
> yalnız `RETURNING`'de taşıyan **kapsamsız** bir write → **KIRMIZI** (kaçıramıyor);
> doğru kapsanmış ve `RETURNING` ile biten bir write → **yeşil** (yanlış alarm yok).
> Yani ayrışma bugün **atıl**; yön yazıldı.
> 🟡 **(N5)** `tags_replaced_by_canonical_hex`'i **hiçbir test adlandırmıyordu**;
> testi yazıldı ve mutasyonla kanıtlandı (kısıt düşünce hata 23514 değil **23503**
> oluyor — yani ateşleyenin FK değil CHECK olduğu da kanıtlandı).
> 🔴 **(N6) `active → retired` GEÇİŞİNDE LOKASYONU NULL'A ÇEKMEK SERBESTTİ** —
> migration'ın *"emekli plaket duvarını korur, audit izi bunu ister"* cümlesini
> tutan tek şey sevk edilen ifadenin **nezaketiydi** (ölçüldü: `UPDATE 1`). Yeni
> kısıt **`tags_retired_keeps_its_location`**. **Güvenli olduğu ölçüldü:** ihlal eden
> **0** satır var; hiçbir sevk edilen akış engellenmiyor (retire `status='active'`
> ister, active zaten lokasyon ister); **`lost` bilinçle dışarıda** — stokta
> kaybolmuş plaket gerçek bir durum (**8** satır). Mutasyon **KIRMIZI**.
> 🟡 **(N7)** 1. tur bloğunun *"muafiyet eklendi"* cümlesine **⛔ geri alındı**
> işareti ve V2'ye ileri gönderme eklendi.
>
> 📌 **R4 ÇOK SATIRLI YAPILMIYOR (orkestratör kararı, 2026-08-09).** Satır-yerellik
> **sayılmış limit** olarak `internal/db/tagsinventory_test.go` başlığında duruyor.
>
> **2200 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** ·
> `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı **dokunulmamış**.

> **Kart düzeltmesi (2026-08-09, M6-06 B veri katmanı — 6. tur, üçüncü RED sonrası).**
> İki bloklayan da **bir önceki turdaki kendi düzeltmemin yan etkisiydi** — ve şekil
> yeni: **bir kısıt eklemek, aynı dosyadaki başka cümleleri geçersiz kıldı.**
>
> 🔴 **(X1) `Down` notu "7 CHECK" diyordu; ölçüm 8.** Sayı **N6'ya kadar doğruydu**;
> `tags_retired_keeps_its_location`'ı 4. turda ben ekledim ve **aynı turda kartıma
> `v13 checks=8` yazdım** — yani doğru sayıyı ölçüp **ölçtüğüm dosyaya taşımadım**.
> Taze bir v13 klonunda reddedilen `Down` yeniden sürüldü: **8** CHECK, trigger 1,
> T11 indeksi 1, goose **v13**. Cümle düzeltildi ve *"bu sayıya güvenme, `pg_constraint`'ten
> say"* uyarısı eklendi.
>
> 🔴 **(X2) BELGELENEN KURTARMA REÇETESİ KENDİ ŞEMAMIN YASAKLADIĞI YOLDU.** Not
> *"`UPDATE … SET status='retired'` → 23502"* diyordu; ölçüm: o UPDATE **`Down`'a
> hiç ulaşmıyor**, **23514 `tags_retired_keeps_its_location`** ile düşüyor — yani
> rollback runbook'unu izleyen operatör **belgelenenden başka** bir hatayla
> karşılaşıyordu. Blok baştan yazıldı; **her satırı taze bir v13 klonunda üretildi**:
> - **iki ayrı engel, iki ayrı ifade:** `unassigned` plaket → **23514
>   `tags_status_check`** · stokta **`lost`** plaket (durumu v12'de yasal, lokasyonu
>   değil) → **23502** `SET NOT NULL`;
> - **işe yarayan iki reçete, ikisi de uçtan uca sürüldü:** `unassigned` → **BAĞLA**;
>   stokta `lost` → **sadece lokasyon ver** (durum `lost` kalabilir — v12 kabul eder,
>   ve yeni kısıt yalnız `retired`'ı bağlar) → `goose down` **exit 0** → v12;
> - **başarısız `Down` atomik**: reddedilen koşumdan sonra şema **bozulmamış**.
> - **Ders dosyaya yazıldı:** *bir kısıt eklerken, o dosyanın kısıt SAYAN ve kısıt
>   DAVRANIŞI TARİF EDEN her cümlesini yeniden ölç.*
>
> 🔴 **(Y3) İKİ RLS ÖZELLİĞİ HİÇBİR TESTLE PİNLİ DEĞİLDİ — pinlendi.** `NO FORCE ROW
> LEVEL SECURITY` **ve** politikanın `NULLIF`'ini çıplak cast'e çevirmek, ikisi de
> `internal/db`'yi **yeşil** bırakıyordu. İkisi de 00004'ün, ama ikisi de §6'nın
> **yazılı** garantileri. Yeni test ikisini de tutuyor; **taze klonlarda iki ayrı
> mutasyon, ikisi de KIRMIZI**.
> - ⚠️ **`FORCE` davranışsal olarak test EDİLEMEZ ve bunu söylemek işin kendisi:**
>   `FORCE` yalnız tablo **sahibini** bağlar, sahip `tappa_owner` **superuser**, o da
>   RLS'i koşulsuz atlıyor (M0-03). Yani sonucu değişen hiçbir sorgu yok → katalog
>   iddiası (`relforcerowsecurity`) **doğru** araç.
> - **`NULLIF` iki yarımla tutuldu:** katalog + **davranış**. Davranışın göründüğü
>   tek yer GUC'un **yazılıp boşaldığı** bağlantı; taze bağlantıda iki yazım da aynı.
>   Mekanizma ayrıca çıplak psql ile kanıtlandı: NULLIF → `context-less rows=0`;
>   çıplak cast → **`ERROR: invalid input syntax for type uuid: ""`**.
>
> 🟠 **(Y4) FAZ A'NIN DEVRETTİĞİ AD SINIRI ŞEMAYA GİRDİ.** Ölçüm: `locations`
> **140 331** satır, 80 rune'u aşan **0** (en uzunu tam 80 — o da bizim pozitif
> kontrolümüz), boş **0**; `departments` **25 372**, aşan **0**, boş **0** → validated
> eklendi. Kısıt `venueName`'in **iki** kuralını da yansıtıyor (trim-boş-değil **ve**
> ≤80 rune); yalnız uzunluğu yazmak `'   '`'ü saklanabilir bırakırdı. **Rune/bayt
> doğrulandı:** `char_length('ċġħż')=4`, `octet_length=8` — test 80 Malta rune'unu
> **kabul**, 81'i **ret** ediyor. Mutasyon KIRMIZI. ⚠️ **80 artık iki yerde** — bedel
> yazıldı, ve `venue.go`'nun *"şemaya yazılmadı"* cümlesi (X2 dersi) güncellendi.
>
> 🟡 **(Y1)** Süreler iyimserdi: yazdığım **21,2 / 24,7 / 26,1** tek bir üç-koşumun
> **sıcak ucu**ydu; bağımsız ölçüm **34,5 / 36,2 / 35,6**. **Altı koşum** yeniden
> alındı → **31,7–38,1 ms (medyan ~32)**, ve *"plan tekrarlanır, mutlak sayı makinenin
> özelliğidir"* yazıldı. Şekil/buffer/satır zaten tutuyordu.
> 🟡 **(Y2)** Kayan mutlaklar tarihe bağlandı: stokta `lost` **8 → 26** (yön, seviye
> değil) · 2-baytlık `aes_key_ref` **18 038 → 19 580 / 78 803**. Küçük harfli uid
> **18 010** ise iki tam koşumdan sonra **değişmedi** — kaynağın kapandığının kanıtı.
> 🟡 **(Y5)** *"three rewind probes"* → *"counter probes"*; ortadaki (501→501)
> **rewind değil idempotent yeniden yazma** ve **geçmesi** bekleniyor.
>
> **2209 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** · `-short` skip **3**
> · `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı **dokunulmamış** · `Down` taze
> klonda: reddediliş **23514** + şema bozulmamış, sonra reçeteyle **13→12→13**.

> **Kart düzeltmesi (2026-08-09, M6-06 B veri katmanı — 7. tur, KAPANIŞ).**
> `tappa-security-auditor` **ONAY** verdi; bu tur onun beş kalanını kapatıyor.
>
> 🔴 **(Z2) GUARDRAIL TERSİNE ÇEVRİLDİ — DEVREDİLMEDİ (kullanıcı/orkestratör kararı).**
> `sys:tag-not-active` artık **`s != active`** (allow-list), eskiden `retired || lost`
> (deny-list) idi ve 00013'ün eklediği **dördüncü** değer `unassigned` ondan
> **kaçıyordu**. **Eşdeğerlik iddia edilmedi, ÖLÇÜLDÜ:** yeni test durum sözlüğünü
> **`db/migrations`'ın CHECK kümesinden türetiyor** (Up yarısından; Down'daki eski
> liste bilerek atlanıyor) ve iki formu **dört değerin dördünde** karşılaştırıyor —
> 00013 öncesi üç değerde **aynı**, `unassigned`'da **farklı**. İki anti-vakum kapısı
> var: `active` vakası yoksa ve **hiç ayrışma yoksa** test kendini reddediyor
> (ikincisi tam olarak *"tersine çevirme yürürlükte değil"* demek).
> **Asıl kazanç bir sonraki değer:** şemaya eklenen beşinci durum, kimse hatırlamasa
> da **eklendiği gün reddedilir**. Mevcut tablo bazlı vakaya `unassigned` eklendi.
> **Mutasyon (deny-list'e dönüş) → İKİ test birden KIRMIZI**, ve çıktı düşüşü
> gösteriyor: `review / default` (fail-to-review).
>
> 🔴 **(Z1) MIGRATION KENDİ RİSKİNİ FAZLA İDDİA EDİYORDU, VE SOMUT OLANI YAZMIYORDU.**
> *"can end as `ok`"* **yanlıştı** — ve şemayı olduğundan kötü gösteren yönde:
> monte edilmemiş plakette lokasyon çözülmediği için **ne IP ne GPS kanıtı** var →
> `base:no-evidence-review` → **en kötü `flag`**, ve **§4.6 korunuyor**. Yazılmayan
> **somut** yol ise QR'dı: **NFC** `preview.go`'nun `status != active` **allow-list**'i
> ile kapalıydı (ama `sys:sun-invalid` üzerinden, yani gerekçe metni *"imza
> doğrulanamadı"* diyor — **yanlış**, ve düzeltmesi uygulama katmanının);
> **`sys:sun-invalid` kanal olarak `nfc`'ye bağlı**, dolayısıyla **QR onu tamamen
> atlıyordu** → kutudaki plaketin uid'ini okuyan biri evinden **müdürün
> onaylayabileceği `flag` satırları** üretebiliyordu. Paragraf bu zincire eşitlendi.
> ⚠️ Z2 bu yolu **her iki kanalda** kapattı (guardrail #2, #3'ten önce).
> ⚠️ Ve *"koruma bir sonraki turda sona eriyor"* cümlem **yanlıştı**: B **bilerek
> hiçbir INSERT yolu sevk etmedi**, yani koruma bu fazda **bitmiyor** — bir yükleyici
> yazıldığı gün bitiyor.
>
> 🟡 **(Z3)** Sözlük iki yerde bayattı (`guardrails.go`, `tap/types.go` — ikisi de
> *"active|retired|lost"* ve **00004**). İkisi de güncellendi (`TagUnassigned`
> eklendi) ve **neden üç temsil olduğu** yazıldı: policy `store`'u **bilerek** import
> etmiyor, ikisi de tap sırasında migration okuyamaz → kopyalar **kaynakta disiplin,
> sınırda TEST** ile hizada tutuluyor (Z2'nin türetimli testi). Üçü de artık
> *"bilinmeyen değer = ret"* yönünde yazılı.
> 🟡 **(Z4)** `Down` bloğu ~60 satırını *"ne zaman çalışmaz"*a ayırıp **çalıştığında
> ne açtığını** söylemiyordu. Yazıldı ve taze klonda ölçüldü: v12'de `tappa_app`
> yine **9 sütunda UPDATE**, `tags_counter_monotonic` **yok** → `aes_key_ref`
> ezilebilir, `last_ctr` **geri sarılabilir** (§4.4 replay penceresi).
> *"Temiz geri alındı"* ile *"güvenli"* ayrı kelimeler.
> 🟡 **(Z5)** R4'ün satır-yerelliği artık **script'in kendi içinde** de yazılı
> (okuyan script'i okur). **Yalnız yorum**: davranış değişmedi ve pozitif kontrolle
> kanıtlandı — üretim SQL'ine kosulsuz sayaç yazımı → **exit 1**, geri alınınca 0.
> 🟡 **(Z6)** `ResolvedTag.LocationID`'nin yorumu **`uuid.Nil` = "monte edilmemiş"**
> diyor artık; pgx'in NULL'ı **hatasız** `uuid.Nil` yaptığı ve bunun **beş
> resolver'ın tek nullable sütunu** olduğu yazılı. Tip değişikliği (→ `*uuid.UUID`)
> **uygulama turunun**.
>
> **2215 PASS / 0 FAIL / 0 SKIP · 16 paket** · `fmt`/`gen`/`lint`/`audit` **exit 0**
> · `Down` taze klonda **13→12→13**.

> **Kart düzeltmesi (2026-08-09, M6-06 B — UYGULAMA KATMANI / panel).** Kartın kalan
> dört kriteri sevk edildi. **Yeni migration YOK** (son: 00013) ve **yeni sorgu da
> YOK** — beş tag sorgusu + `ConfirmRecentRemoval` + `GetTenantClock`, hepsi zaten
> vardı. Panelin sözlüğü **listele · oku · bağla · emekliye ayır**; `tags`'a INSERT
> eden hiçbir şey eklenmedi.
>
> 🔴 **KART BİR ŞEYİ EKSİK YAZIYOR VE DÜZELTİLİYOR: "replace tag" TEK BAŞINA
> YETMİYOR.** Kriter *"yeni UID kaydedilir"* diyor; envanter modelinde panel kayıt
> **etmez**, **bağlar** — ve bir tenant'ın İLK plaketinin bir duvara çıkması
> replace değil **MOUNT**'tur. Sevk edilen iki eylem: **mount** (stoktaki plaketi bir
> lokasyona bağla) ve **replace** (duvardakini emekliye ayır + halefini AYNI duvara
> bağla, **tek transaction**). Brief'in kendi cümlesi de ikisini adlandırıyor
> (*"bağlanmamış bir plaketi lokasyona atamak ve eskisini retire etmek"*).
>
> 📏 **30 SANİYE ÖLÇÜLDÜ** (gerçek HTTP + gerçek Postgres, `plaquejourney_db_test.go`,
> üç koşu): **3 ekran, 2 tıklama, sunucu süresi 176 / 184 / 195 ms** (liste → kart →
> POST → bildirim). Mount: **2 ekran, 2 tıklama, 120–159 ms**. ⚠️ Bu **sunucu**
> süresidir; müdürün 30 saniyesi merdiveni de içerir. Bu yüzden test **şekli** de
> pinliyor (ekran ve tıklama sayısı): asıl bütçeyi yiyecek olan, müdüre venue'yu
> yeniden sordurmak ya da yedek plaketin uid'ini elle buldurmaktır — replace formu
> duvarı **emekliye ayrılan plaketten** okur ve yedeği açılır listede sunar.
>
> ✅ **K3 — GERİ ALINABİLİRLİK ÖLÇÜLDÜ, VE SONUÇ "MAC'li onay + owner-only" DEĞİL.**
> Canlı ölçüm (2026-08-09, `tappa_app`, geri alınan transaction içinde):
> `DELETE FROM tags` → **permission denied** · retire → **UPDATE 1** · `retired →
> active` + damgaların temizlenmesi → **UPDATE 1** · `active → unassigned` +
> `location_id=NULL` **tek ifadede** → **UPDATE 1** · yeniden mount → **UPDATE 1**;
> satır sonunda aynı uid, aynı duvar, damga yok, `aes_key_ref` **44 bayt**. Yani
> **hiçbir satır yok edilmiyor** ve bu eylemlerin yazdığı **her sütun şema düzeyinde
> geri çevrilebilir**. Faz A'nın onay+owner kapısı **tek bir özellikten**
> gerekçelendirilmişti — *"DELETE satırı sonsuza dek yok eder, arşiv yok"* — ve
> ikisi de burada geçerli değil.
> - ⚠️ **KARŞI OKUMA, GÖMÜLMEDEN YAZILIYOR:** o tersleri **yapan bir ROTA sevk
>   edilmedi** (`db/queries`'te un-retire ve un-mount yok, bu tur da eklemedi). Yani
>   *"geri alınabilir"* = **şema izin verir ve hiçbir delil kaybolmaz**, *"müdür
>   panelden geri alabilir"* **değil**. Onayın yerini tutan şey eylemin **şekli**:
>   replace **aynı tenant'ın gerçek bir STOK plaketini adlandırmak** zorunda ve her
>   iki yarı da ön koşulunu **kendi WHERE'inde** taşıyor → tekrar edilen ya da
>   tahmin edilen bir POST **sıfır satır** eşleştirir ve **hiçbir şey yazmaz**.
> - 📌 **Karar orkestratörün/kullanıcının:** Faz A'nın kapısı burada da isteniyorsa
>   sayılar yukarıda. Uygulanması `deactivateconfirm.go`'nun payload'ını **v3**'e
>   taşımayı gerektirir (özne bir `uuid` değil **14 haneli uid**), yani sevk edilmiş
>   ve denetlenmiş bir güvenlik primitifine dokunmak demektir — bu turda **bilinçle
>   yapılmadı**.
>
> 🔴 **ON YÜKÜMLÜLÜĞÜN DURUMU.** **(1) KAPANDI** — `preview.go`'nun 3. adımı artık
> allow-list olduğunu, `cmacOK=false`'un *"CMAC hiç SORULMADI"* demek olduğunu ve
> gerekçenin **durum** olduğunu yazıyor; ayrıca `sys:tag-not-active`'in metni
> *"this tag is no longer active"* → **"this tag is not in service"** oldu (eski
> lafız `unassigned` için **yanlıştı**: hiç aktif olmamış bir plaket). Uçtan uca
> pinli. **(2) KAPANDI** — `db.ResolvedTag.LocationID` artık **`*uuid.UUID`**;
> `sun.Preview.Location` ve `sun.Result.Location` da öyle. `tap.Tag.Location`
> **düz kaldı** ve düzleştirme **paket başına bir tane olmak üzere iki adlandırılmış
> yerde** (`tappedWall` — internal/domain/checkin, `tappedWallOf` — internal/handler;
> ikisi ayrı paket olduğu için tek fonksiyon paylaşılamıyor) gerekçesiyle duruyor: §5 satır 1 `active` dışındaki her durumu
> reddediyor ve `tags_active_requires_location` aktif bir plaketi lokasyonsuz
> bırakmıyor, yani düzleştirme **yapısal olarak tamdır**. **(3) KAPANDI** —
> `plaquetap_db_test.go`: **üç durum × iki kanal**, hepsi `verdict=reject`,
> `matched_sid=sys:tag-not-active`, satır **YAZILIYOR** (§4.6), artı **pozitif
> kontrol** (aynı bozuk CMAC, **aktif** plakette → `sys:sun-invalid`). Guardrail'i
> deny-list'e geri çeviren mutasyon **`unassigned`'ın iki alt testini de KIRMIZI**
> yaptı, `retired`/`lost` yeşil kaldı — deny-list'in kör noktası tam olarak orası.
> **(4) KAPANDI** — iki okuma raporlandı, **üç liste** seçildi (duvarda · stokta ·
> hizmet dışı); SQL'in `NULLS FIRST` sırası korunuyor ama monte edilmiş yarıyı
> **location uuid'ine** göre sıralıyor, yani sayfanın hiç basmadığı bir değere göre.
> **(5) KAPANDI** — birleştirme **Go'da, anahtar varlığıyla**; `LastSeen *time.Time`,
> `nil` = *"hiç taplanmadı"*, ekranda **"Never tapped"**. **(6) LİMİT OLARAK
> SAYILDI** — ekran *"Encoded"* derken **`location_id IS NULL`'a bakıyor**, anahtara
> **değil**; kelimeyi mümkün kılan şey `aes_key_ref bytea NOT NULL` (00004) ve
> yükleyicinin tekliği. Zarfın **44 bayt olduğunu hiçbir şey doğrulamıyor** (T7) ve
> bu ekran doğrulayamaz da — doğrulamak sütunu SELECT etmek olurdu, ki §4.7 tam
> olarak onu yasaklıyor. Üç yerde yazılı. **(7) KAPANDI + ÖLÇÜLDÜ** — retire+bind
> **tek `WithTenant`**; ara durumun imkânsızlığı **gerçek başarısızlıkla** sürülüyor
> (halef stok değil → retire **geri alınıyor**, `audit_log`'a **0 satır**). ⚠️
> **Mutasyon iki denemede kapandı ve bu ölçüm rapora giriyor:** bind'i **iç içe**
> bir `WithTenant`'a taşımak testi **KIRMIZI YAPMADI** (dış transaction yine geri
> alıyor); gerçek şekil — retire'ı **ayrı bir transaction'da COMMIT edip** sonra
> bind denemek — **KIRMIZI**. **(8) KAPANDI** — `tenant.PlaqueUID` sınırda:
> `^[0-9A-F]{14}$`, `ToUpper`+trim; `::char(14)`'ün **sessizce kırptığı** şekil
> (`AABBCCDDEEFF01ZZZZZZ`) handler'da reddediliyor ve **domain hiç çağrılmıyor**
> (testte çağrı sayısıyla ölçülü). **(9) DEVREDİLDİ (T16)** — bu tur `tags`'a
> **hiçbir INSERT** eklemedi; koruma bir yükleyici yazıldığı gün biter.
> **(10) DOKUNULMADI** — `ledger`'ın `stopRE`'si atıl, bilgi olarak duruyor.
>
> 🟡 **KENDİ AĞLARIM İKİ YERDE BAYATLADI VE İKİSİ DE DÜZELTİLDİ (kapsam işareti).**
> (a) `TestVenueVocabulary_HoldsOnlyWordsTheServerEmits` **yalnız
> `locationactions.go`'yu** okuyordu; sekiz canlı kelimeyi *"ulaşılamaz"* diye
> bildirdi — ağın **kapsamı bayatlamıştı**, kelimeler değil. Tarayıcı üç dosyaya
> genişletildi ve `plaqueRefusal`'ın kelimeleri **türetiliyor** (elle listelenmiyor);
> `same-plaque`'i emitten silen mutasyon **KIRMIZI**. (b) `query_test.go`'nun
> *"iki küme AYRIK"* cümlesi bu turda **yanlış oldu** (artık bu paket beş tag
> sorgusunu çağırıyor) → **birleşim hesaplanıyor**: `25 of 65 (%38,5)`.
>
> 🟡 **TİP DUVARININ YAZILI KAPSAMI ÖLÇÜLDÜ VE BİR OVERCLAIM DÜZELTİLDİ (K1).**
> `plaqueview.go` *"şu struct'ları yürüyor"* diyordu ama adlandırdığı test
> **domain paketindeydi** ve render tiplerine **ulaşamaz** (domain render'ı import
> edemez). İkinci yürüyüş `internal/handler`'a eklendi (`components.Plaque*` **artı
> `pages.LocationsView`**), muafiyetler **gerekçeli** ve **ulaşıldığı doğrulanan**
> bir listede (bayat muafiyet = hata). Üç mutasyon **KIRMIZI**: domain tipine
> masum adlı `[]byte` · render tipine `AESKeyRef string` · `tags.sql`'de
> `aes_key_ref` SELECT'i.
>
> 🟡 **K8 — DONMUŞ SATIR: DURUM ÖLÇÜLDÜ.** Bir CHECK **her rolü** bağlar, yani
> küçük harfli yeni satır **`tappa_owner` olarak bile** yazılamıyor (23514, ölçüldü)
> — donmuş satırlar yalnız 00013 **öncesinden** kalanlar ve başka tenant'lara ait,
> RLS onları fixture'dan **doğru şekilde** gizliyor. Bu yüzden pinlenen şey
> **haritalama**: 23514 → `ErrPlaqueFrozen` → cümle (500 değil), ve anti-vakum
> olarak 23503 → `ErrUnknownVenue`. Ekran tarafında satır **listeleniyor ama
> kontrol sunulmuyor** ve nedeni yazıyor.
>
> **2294 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** · `-short` skip **3**
> · `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı (`scripts/redline-check.sh`)
> **dokunulmamış** · `make simulate-day` **PASS** (10 çalışan, 31 kayıt) ·
> `make css` sonrası `app.css` **diff yok** (yeni utility gerekmedi).

> **Kart düzeltmesi (2026-08-09, M6-06 B paneli — 2. TUR, üçüncü göz RED sonrası).**
> Üç bloklayanın üçü de kapandı; ikisi **bu turun kendi cümlelerini** yanlışladı.
>
> 🔴 **(A1) FAZ A'NIN KUSURU BİREBİR GERİ AÇILMIŞTI — kaynağı TEK BİR `ToUpper`.**
> Sınır fonksiyonu bir **arama anahtarını** normalize ediyordu; `tags.uid` `char(14)`
> ve **harfe duyarlı**, 00013'ün kanonik CHECK'i **NOT VALID** olduğu için **18.010**
> küçük harfli satır duruyor ve `ListTagsForTenant` onları döndürüyor. Sonuç canlı
> ölçüldü (tenant `2f2bb0e2…`, plaket `b27188a6d32e49`): liste satırın **kendi**
> yazımıyla bağlantı basıyor, bağlantı *"bu işletmenin değil"* cevaplıyordu.
> **Sevk edilen:** sınır **ikiye ayrıldı** — `PlaqueUID` (**YAZILACAK** değer:
> yalnız kanonik; `@replaced_by`'ın SQL yükleminin istediği biçim) ve `PlaqueRef`
> (**ARANACAK** anahtar: iki yazım da, **korunarak**). İki farklı soru, iki fonksiyon.
> - **(c) `plaque-frozen` ARTIK ÖLÜ KOD DEĞİL VE UÇTAN UCA SÜRÜLDÜ.** Denetçinin
>   adlandırdığı **gerçek kalıntı satırın** üstünde: `tappa_owner` ile bir donmuş satır
>   **bulunuyor** (üretimde sıfır), o tenant'a bir admin kaydedilip **panelden** giriş
>   yapılıyor, kart **açılıyor**, POST **`problem=plaque-frozen`** ile dönüyor — **500
>   değil**. ⚠️ Kalıntı temizlenirse (T15) test **skip etmiyor**: neyin hâlâ pinli
>   olduğunu yazıp geçiyor.
> - **Mutasyon:** `PlaqueRef`'e `ToUpper` geri konunca **dört test** kırmızı
>   (bölüm · POST sınırı · uçtan uca donmuş satır · iki-sınır tablosu).
> - Yanlışlanan üç cümlenin üçü de düzeltildi (`OpenHref` *"boş olabilir"* iddiası
>   dahil; **hiç boş olmuyor** ve satırın **kendi** yazımını taşıdığı artık yazılı).
>
> 🔴 **(A2) ONAY ADIMI SEVK EDİLDİ (orkestratör kararı, 2026-08-09).** 1. turun K3
> ölçümü doğruydu ama **yanlış soruyu** cevaplıyordu: Faz A'nın kapısı *"satır yok
> oluyor"* diye değil, *"müdür geri getiremiyor"* diye vardı — ve benim kendi karşı
> okumam (*"hiçbir ROTA tersini yapmıyor"*) tam olarak bunu söylüyordu.
> - **`deactivateconfirm.go` payload v2 → v3**, ikinci kalıp **icat edilmedi**: özne
>   `uuid`'den **opak dizeye** genişletildi (plaketin kimliği 14-hex uid). Bu bir
>   **alan genişletmesi**, garanti değil — değer hâlâ MAC içinde, uzunluk önekli,
>   sabit zamanlı karşılaştırılıyor. `uuid` tipinin bedava verdiği **tek** özellik
>   (özne ayracı içeremez) artık **açık bir kontrol**.
> - **`owner`-only UYGULANMADI** ve gerekçesi yazıldı: Faz A'nın rol kapısı **kalıcı
>   silme** içindi; plaket değişimi sabah altıda kapının önünde duran şube müdürünün
>   **operasyonel bakımı**.
> - **Kartın uyarısı:** onay **ikinci bir ekran DEĞİL** — kart **zaten** bir seçim
>   (hangi yedek) istiyor, o yüzden jeton **kartın kendi GET'inde** basılıyor ve
>   uyarı kartın içinde. Mekanizmanın satın aldığı özellik aynen korunuyor:
>   *"yazmaya ulaşmak, sunucunun uyarıyı BU oturuma, BU kişiye, BU pencerede
>   RENDER ETMİŞ olmasını gerektirir"*. **3 ekran / 2 tıklama değişmedi.**
> - **Mount onaysız kaldı ve bu ÖLÇÜLDÜ, varsayılmadı:** dört alt test — onaysız
>   replace **reddediliyor ve domain'e ulaşmıyor** · mount **geçiyor** · mount kartı
>   **jeton basmıyor** (basan bir kapı, hiç harcanmayan bir kapıdır) · başka plaket
>   için basılmış jeton **reddediliyor**. **İki yönlü mutasyon:** kapıyı silmek →
>   replace yarısı KIRMIZI; mount'u da kapılamak → mount yarısı KIRMIZI.
> - **v2 jetonu reddediliyor** (sunucunun **kendi anahtarıyla** imzalanmış bir v2
>   payload'ı ile ölçüldü, pozitif kontrolü v3 ile) · **çapraz-eylem matrisi dörde
>   çıktı** (`plaque.replace` eklendi).
>
> 🔴 **(A3) §4.7 DUVARI DENYLIST'TEN ALLOW-LIST'E ÇEVRİLDİ.** Denetçinin üç şekli —
> `Material [44]byte` (**zarfın tam boyu**, dizi), `Ref string` (nötr ad),
> `Extras map[string][]byte` — eski duvarın **üçünden de** geçiyordu. Yeni duvar iki
> allow-list: **alan ADLARI** tip başına **birebir sayılıyor**, ve **alan ŞEKİLLERİ**
> izinli **kind** kümesine karşı **her derinlikte** yürünüyor → dizi, map, arayüz,
> kanal ve `[]byte` **şekilden** reddediliyor. `uuid.UUID` **kimlikle** izinli
> (kendisi `[16]byte`) — `[44]byte`'ı reddedebilmenin tek yolu bu.
> - **Üç mutasyonun üçü de KIRMIZI**, ve ikisi **hem ad hem şekil** kolundan.
> - **Var olmayan test adı** düzeltildi; `plaque.go` · `plaqueview.go` ·
>   `locationsview.go`'daki **mutlak cümleler ölçüye indirildi** ve
>   *"KAPSAM DIŞI"* listelerine **dördüncü madde** eklendi: **izinli bir alana
>   KODLANMIŞ anahtar** (dizede hex). Bunu hiçbir tip sistemi reddetmez; onu
>   erişilemez kılan şey, bu yolda hiçbir sorgunun sütunu **SELECT etmemesi**.
>
> 🟡 **BLOKLAMAYANLARIN HEPSİ KAPANDI.** **C1** `VenueUnknown` alanı eklendi —
> *"monte değil"* ile *"mekân adı okunamadı"* artık iki ayrı cümle (cümleyi
> yumuşatmak yerine **davranış** eklendi). **C2** mount yolculuğu **listeden
> başlıyor** ve ekran/tıklama **sayıyor** (sabit dize kaldırıldı): **3 ekran, 2
> tıklama**. **C3** her süre **popülasyonla**: harness tenant'ı **1 mekân / 1-2
> plaket / 0 işlem → 137-160 ms**, ve **yeni bir test** aynı yolculuğu **seed'li KF
> tenant'ında** sürüyor — `10000000-…-0001`, **9 mekân, 11+ plaket, 30.747 işlem →
> 203 / 246 / 350 ms** (denetçinin 191,9 ms'iyle aynı mertebede). **C4** iki audit
> yükü **birebir anahtar kümesiyle** pinli (mutasyon: yeni alan → KIRMIZI).
> **C5** `dashboard.go` sayıları **10 / altı**. **C6** kelime tarayıcısı artık
> **yalnız `plaqueRefusal`'ın gövdesini** okuyor (parantez sayan yardımcı).
> **C8** fixture **gerçek 44 baytlık zarf** yazıyor (uzunluğu da doğruluyor) —
> `'\xDEAD'` kalıntısı artmıyor. **C9** kart artık *"paket başına bir tane, iki
> adlandırılmış yer"* diyor.
>
> ✅ **C7 — KARARINIZ UYGULANDI: GEÇMİŞ ARTIK `audit_log`'DAN DA OKUNUYOR.**
> **Yeni sorgu var** ve *"bu turda yeni SQL yok"* iddiam **düştü** — doğrusu bu:
> `ListPlaqueHistory` (migration YOK). Kart artık **iki kaynağı** ve **iki işi**
> yazıyor: `tags` satırı **NE ve NE ZAMAN**'ı (yüklendi · şundan devraldı · emekli ·
> şununla değişti) **eksiksiz** veriyor ve **aktör sütunu YOK**; `audit_log`
> **KİM**'i veriyor — müdürün bir kapı bozulduğunda geldiği asıl soru. Kartta *"Who
> did what"* bloğu: eylem · zaman · **admin_users'tan JOIN'lenen ad** (izde saklanan
> ad yazıldığı anın adı olurdu ve kayardı), `actor_id` NULL olan sistem olayı için
> *"by the system"*. **Liste bunun için hiçbir şey ödemiyor** (sorgu sayısıyla
> ölçülü: liste **0**, kart **1**), okuma patlarsa **500** (§4.6 — *"bu plakete
> hiçbir şey yapılmadı"* bir iddiadır). Mutasyon: kart izi okumayı bırakınca KIRMIZI.
>
> **2312 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** · `fmt`/`gen`/
> `lint`/`audit` **exit 0**, tarayıcı **dokunulmamış** · `make simulate-day` **PASS
> (64,2 sn)** · kuşak: bu paketin çağrı-türetimli belt'i **25/66**, tags.sql
> dosya-türetimli **6/66**, **birleşim 26/66 (%39,4)**.

> **Kart düzeltmesi (2026-08-09, M6-06 B paneli — 3. TUR, ikinci üçüncü göz RED sonrası).**
> İki bloklayan da **bir önceki turun kendi kararının/ağının** yarısıydı.
>
> 🔴 **(D1) MOUNT GERİ ALINAMAZDI, VE BUNUN TERSİNİ SÖYLEYEN İKİ CÜMLE *"ÖLÇÜLDÜ"*
> ETİKETİ TAŞIYORDU.** Denetçi uçtan uca sürdü: yanlış duvara mount → **303**;
> sonrasında kart **hiçbir form** sunmuyor; doğru kapıya yeniden mount →
> **`plaque-not-stock`**; stokta yedek yokken ekranın tek cümlesi *"Ask us for one"* —
> müdür plaketi **elinde tutarken**. Bu arada o kapıdaki her tap **yanlış mekânın**
> IP/GPS'ine karşı ölçülüyor → **§5 satır 7 → onay kuyruğu**. Şema tersine hep izin
> veriyordu; **ürünün ifadesi yoktu**.
> - ✅ **KARARINIZ UYGULANDI: GERİ ALMA YOLU SEVK EDİLDİ.** Yeni sorgu
>   **`UnmountTagFromWall`** (migration YOK): `location_id = NULL` + `status =
>   'unassigned'` **tek ifadede** — 00013'ün iki CHECK'i ara durumu zaten yasaklıyor.
>   **Emeklilik DEĞİL:** `retired_at`/`replaced_by`'a **dokunulmuyor**, yani *"bu
>   plaket duvarı ne zaman TEMELLİ terk etti"* sorusunun hâlâ **tek** cevabı var ve
>   bir hatayı düzeltmek **yedek harcamıyor**.
> - **Geri alma yolu KENDİSİ hizmet dışı bırakıyor** (duvardan inen plaket
>   `unassigned` → §5 satır 1), o yüzden **replace ile aynı onay kapısını** taşıyor:
>   aynı ilkel, **beşinci eylem** (`plaque.unmount`), ikinci mekanizma değil.
> - **MOUNT KAPISIZ KALDI VE CÜMLE ARTIK DOĞRU.** Bir mount tek basışta, yedek
>   harcamadan geri alınıyor.
> - **Ekran da düzeldi (kararın "her hâlükârda" maddesi):** mounted bir plaketin
>   kartı **her zaman** *"On the wrong door? … Take it off the wall…"* sunuyor, ve
>   yedeksiz hâldeki cümle artık *"take it off the wall below and mount it where it
>   belongs — that costs no spare"* diyor. **Yedeksiz kartın hiçbir şey sunmaması
>   sorunu bununla kapandı** (testle sürülü).
> - **Uçtan uca ölçüldü:** yanlış mount → kart → uyarı → POST → **doğru kapıya**
>   yeniden mount = **3 ekran, 3 tıklama, 198–207 ms** (tenant + mekân + plaket +
>   işlem sayısıyla loglanıyor). Mutasyon: un-mount kapısını silmek **iki alt testi**
>   kırmızı yapıyor.
>
> 🔴 **(D2) *"MOUNT KARTI JETON BASMIYOR"* AĞI, BASMAYI DEĞİL RENDER ETMEYİ
> ÖLÇÜYORDU.** Denetçinin MUT5'i (kartı jeton basar yap) **derlendi, uygulandı** ve
> `internal/handler` **2312 test yeşil** kaldı: iki assertion da **HTML gövdesini**
> tarıyordu, `.templ` gizli input'u yalnız `replace` kolunda basıyordu — yani sunucu
> **imzalayıp çereze yazıp hiç kontrol etmeyebilirdi**. Assertion artık
> **`Set-Cookie` / `adminConfirmCookieName`** üstünde (`mintedConfirmation`), ve
> **MUT5 KIRMIZI**. Üstüne bir invaryant eklendi ve testle sürüldü: **render başına
> TAM BİR onay basılır** — çerez tarayıcı başına tek olduğu için iki silahlı form
> taşıyan bir kart, gönderilmeyen formu **kullanılamaz** bırakırdı (yalnız
> başarısız olabilecek kontrol). Un-mount bu yüzden **link + kendi modu**.
>
> 🟡 **E1 — `PlaqueUID`/`PlaqueRef` AYRIMI ARTIK TİP.** Denetçi ölçtü: ikisi de
> `(string, error)` döndürdüğü için çağrı yerinde takas **derleniyor ve tüm suite
> yeşil** kalıyordu; gerçek mekanizma `Replace`'in domain içinde **yeniden**
> doğrulaması idi ama yorum **validator seçimini** mekanizma gibi sunuyordu
> (*"şekil mekanizma değildir"* — bu görevin kendi dersi). `PlaqueUID` artık
> **`CanonicalUID`** döndürüyor ve `ReplaceCommand.SuccessorUID`'in tipi o.
> **Denetçinin MUT2'si artık DERLENMİYOR**; derlenen varyantı (arama anahtarını tipe
> **cast** etmek) **yeni bir test satırıyla KIRMIZI** — küçük harfli bir successor,
> aranabilir ama **yazılamaz** olan tam vaka. Ve tipin **neyi garanti etmediği**
> yazıldı: `CanonicalUID("saçma")` bu pakette inşa edilebilir, run-time güvence
> domain'in yeniden doğrulaması.
>
> 🟡 **E5 — BOŞ AD İLE SİSTEM AYRILDI.** `admin_users.full_name` `text NOT NULL` ve
> **boş-değil CHECK'i yok**, `audit_log.actor_id` ise ayrıca nullable — ikisini `""`
> üstünden birleştirmek, **adı boş bir yöneticinin eylemini ÜRÜNE** yazardı, hem de
> müdürün *"bunu kim yaptı"* diye geldiği tek yerde. Sorgu artık
> **`(a.actor_id IS NULL) AS by_system`** döndürüyor; ekran *"by the system"* ile
> *"by an administrator with no name set"* diye iki ayrı cümle basıyor, ikisi de
> testle sürülü.
>
> 🟡 **E2/E3/E4 — SAYAN CÜMLELER SAYMAYI BIRAKTI.** Plaket sayıları (11 → **63**,
> 1,02 → **1,03**, 78.919 → **83.011**) yeniden ölçüldü **ve bozanın kendi
> testlerimiz olduğu yazıldı** (journey testleri paylaşılan demo tenant'a plaket
> tohumluyor, `tags` silinemiyor) — sayı **yön**, seviye değil (00013'ün `lost`
> için yazdığı aynı kayıt). Tip **sayan** iki cümle (*"üç tip"*, *"Plaque,
> PlaqueScreen ve Replacement"*) **türetime** çevrildi: kök listesi testte yaşıyor.
> Kartın **`25 of 65`** kopyası da bayattı; bugün **birleşim 27/67 (%40,3)** ve bu
> **çalışma anında hesaplanıyor** — kart bir daha sayı kopyalamıyor.
>
> ⚠️ **RAPORUMUN ZEMİNİ HAKKINDA:** çıplak `go test` bu repoda **328 alt testi
> sessizce atlıyor** (exit 0). Bu turun bütün ölçümleri `make`/`.env` üzerinden
> koştu; denetçinin uyarısı karta yazılıyor ki bir sonraki tur aynı zemine
> basmasın.
>
> **2324 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** · `-short` skip **3**
> · `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı **dokunulmamış** ·
> `make simulate-day` **PASS** · `app.css` **diff yok** · migration dizini
> **dokunulmamış**.

> **Kart düzeltmesi (2026-08-10, M6-06 B paneli — 4. TUR, üçüncü RED sonrası).**
> Üç bloklayanın **kökü tekti** ve düzeltme tek tek yama değil, **iki türetim**.
>
> 🔴 **ORTAK KÖK: ELLE BAKILAN BİR N LİSTESİ, N BÜYÜYÜNCE DELİK AÇIYOR — bu görevde
> ALTINCI kez.** Önceki beşi: guardrail **denylist**'ti (dördüncü durum altından
> geçti) · §4.7 taraması **yedi yasaklı kelimeydi** · tip duvarı **denylist**'ti ·
> `venueDoneWords` **elle sayılmıştı** · ve bu turda **eylem sözlüğü** (iki/üç) ile
> **rota listesi** (iki/üç). Yamalamak altıncıyı kapatır, yedinciyi kapatmaz.
>
> ✅ **TÜRETİM 1 — EYLEM SÖZLÜĞÜ (F1).** Panel müdüre **ham veritabanı kelimesi**
> basıyordu (*"plaque.unmounted 10 Aug 2026 02:17 · E2E Owner"*), üç yazılı cümle
> tersini söylerken ve **hiçbir test görmezken**. `switch` yerine tek bir
> `plaqueActionWords` haritası var; **eksiksizliği** `go/ast` ile
> `internal/domain/tenant/plaque.go`'nun **kendi const bloğundan** türetilen bir test
> zorluyor (`ActionPlaque*` sabitlerinin **değerleri**). Fazlalık da yakalanıyor:
> domain'in yazamayacağı bir eyleme kelime koymak da KIRMIZI. **Mutasyon:** dördüncü
> bir eylem sabiti eklemek (kelimesiz) → **KIRMIZI**.
>
> ✅ **TÜRETİM 2 — YAZMA ROTALARI (F2).** İki ağ da `{mount, replace}` **çiftini elle**
> dolaşıyordu; `plaqueUnmountHref` **ikisinde de yoktu**, ve denetçinin MUT9'u
> (rotayı `mountSections`'a = **OKUMA zincirine** taşımak) **derlenip** paketi
> **tamamen yeşil** bırakıyordu — çapraz-origin POST Origin kapısını geçip **oturum
> bütçesini harcarken**. Ağlar artık **gerçek router'ı `chi.Walk` ile geziyor** ve
> `/admin/locations/` altındaki **her POST'u** sürüyor. Bugün **yedi rota**
> (Faz A'nın dördü **dahil** — onlar da ilk kez bu özellik altında). **Mutasyon:**
> MUT9 → **KIRMIZI**, ve hata mesajı sebebi söylüyor (`?problem=confirm-required`,
> yani resolver koştu). **Yeni bir rota eklendiğinde otomatik kapsanıyor.**
>
> 🔴 **(F3) VE BU BENİM 3. TURDAKİ YANLIŞ İDDİAMDI.** *"İki paragraf yeniden
> yazıldı"* dedim; **yazılmamışlardı** — ve ikisi de düzeltmeyi **içeren** dosyalarda,
> **`(measured)` etiketiyle** duruyordu (`plaque.go`, `Unmount`'un ~200 satır
> yukarısında; `plaqueactions.go`, rotanın ~60 satır yukarısında). Bu turda her
> düzeltme `grep` ile doğrulandı; iki paragraf da artık **ne olduğunu ve neyin
> değiştiğini** yazıyor: bir **RETIRE** hâlâ geri alınamıyor (kapının gerekçesi),
> bir **MOUNT** artık alınabiliyor (kapısızlığının gerekçesi).
>
> 🟡 **N1 — ON BAYAT SAYIM, ÇOĞU SAYMAYI BIRAKARAK KAPATILDI.** *"Two routes, two
> acts"* · *"THERE IS NO THIRD ACT"* · *"BOTH ROUTES"* · *"five shipped tag queries"*
> (×2) · *"its two writes"* (×2, biri **Faz A'nın** `locations.go`'sunda ve artık
> yedi) · *"the two acknowledgement words"* · *"THE TWO PLAQUE ACTS"* · *"THE TWO
> WORDS"* · sözlük cümleleri (**UN-BIND** eklendi, ×3). Sayı **gereksiz olan her
> yerde silindi**, gerekli olan yerde **türetime** bağlandı.
>
> ✅ **N2 — AÇIK GERİ ÇEKME:** bu kartın *"yeni sorgu da YOK"* cümlesi (bu bloğun
> üstünde, B panelinin ilk düzeltmesinde) **artık geçerli değil**. **İKİ yeni sqlc
> sorgusu sevk edildi** — `ListPlaqueHistory` (3. tur, C7) ve `UnmountTagFromWall`
> (3. tur, D1). **Migration hâlâ YOK** (son: 00013).
>
> 🟡 **N3 — `Replace`'in yeniden doğrulaması PİNLENDİ.** Denetçi o satırı sildi:
> **derlendi**, domain **ve** handler **yeşil** kaldı — yorumu ise *"THE REAL
> MECHANISM … run time'da reddeden budur"* diyordu. Yeni test küçük harfli bir
> successor'ı **tipe cast ederek** geçiriyor ve `ErrPlaqueUID` **artı hiçbir yazma**
> istiyor; pozitif kontrolü kanonik successor. **MUT3 → KIRMIZI**, ve hata mesajı
> denetçinin teşhisini birebir doğruluyor: satır olmadan **SQL yüklemi** yakalıyor ama
> müdür *"retire matched no row"* iç hatasını görüyor. İki katman, biri **doğru cümleyi**
> söylüyor.
>
> 🟡 **N4 — TİP DUVARI KOMUT TİPLERİNE UZATILDI.** Denetçi `UnmountCommand`'a
> `[]byte` + `[44]byte` ekledi, duvar **yeşildi**: testin adı *"NoTypeOnThisPath"*
> iken kökleri yalnız **okuma** tipleriydi. Komutlar aynı yolu **ters yönde**
> geziyor, o yüzden kök oldular (liste genişletildi, *"NOT COVERED"* değil).
> **Mutasyon → KIRMIZI**, hem ad hem şekil kolundan.
>
> 🟡 **N6/N7 — SAYILAR YA ARALIK VE POPÜLASYONLA, YA HİÇ.** Undo yolculuğu **beş
> koşu: 142 / 144 / 145 / 153 / 225 ms** (harness tenant'ı: 2 mekân, 1 plaket, **0
> işlem**) — kartın eski *"198–207 ms"*'i denetçinin **167,8 ms**'ini kapsamıyordu.
> Plaket popülasyonu (**63 → 93 bir günde**) artık **hiç yazılmıyor**: sürükleyen şey
> bu görevin **kendi** journey testleri, ve seviye bir sonraki okur gelmeden bayatlıyor
> — yorum **şekli** yazıyor (bir plaket kapı pervazına vidalanır, liste **onlarca**),
> sayıyı isteyen tablodan saysın.
>
> ⚠️ **RİTİM NOTU, açıkça:** 3. turda *"yeniden yazıldı"* diye bildirdiğim iki
> paragraf yazılmamıştı ve bunu **denetçi** ölçtü. Bu turda her *"düzeltildi"* cümlesi
> `grep` çıktısıyla kanıtlandı ve ağların ikisi de **kendi mutasyonuyla** sürüldü.
>
> **2331 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** · `-short` skip **3**
> · `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı **dokunulmamış** ·
> `make simulate-day` **PASS** · kuşak **birleşim 27/67 (%40,3)** · migration dizini
> **dokunulmamış**.

> **Kart düzeltmesi (2026-08-10, M6-06 B paneli — 5. TUR, dördüncü RED sonrası).**
> Denetçinin cümlesi tam yerindeydi: ***"F1 üründe kapalı — kapatan ağ değil."***
>
> 🔴 **(G1) TÜRETİM 1 İKİ ŞEKİLLE YENİLDİ, VE İKİSİ DE YAZILMAMIŞTI.** Tarayıcı
> *"tam olarak `internal/domain/tenant`'ın yazabileceği küme"* diyordu — **paket**
> iddiası, **tek dosya** taraması — ve o paket audit sabitlerini zaten **üç dosyaya**
> (venue.go'da **420 satır arayla iki blok**) dağıtmış durumda.
> - **Sevk edilen:** `parser.ParseDir` ile **test dışı tüm paket**; ve `BasicLit`
>   olmayan bir değer artık **sessizce atlanmıyor, yüksek sesle düşüyor** — çünkü
>   sessizlik bir türetimin veremeyeceği tek cevap: *"orada bir şey yoktu"*tan
>   ayırt edilemez. **MUT-A2** (sabit `venue.go`'da) ve **MUT-A4**
>   (`"plaque." + "lost"`) artık **KIRMIZI**.
> - **Ve tarayıcının GÖREMEDİĞİ şey için üçüncü bir ağ eklendi:** eylem satır içi bir
>   dize olarak yazılırsa hiçbir sabit taraması onu duymaz. Yeni test
>   `audit.Event{Action: …}` alanlarını **AST'den** okuyup literal olanı reddediyor
>   (**MUT-A5 → KIRMIZI**). Kalan limit yazılı: **adı `ActionPlaque` ile başlamayan**
>   bir sabit, ve **çalışma anında kurulan** bir eylem dizesi.
> - 🟡 **VE ANTİ-VAKUM CÜMLEM MEKANİZMAYLA ÇELİŞİYORDU** (ikisinde de): taban
>   *"bugünün altında, böylece silme yüksek sesle düşer"* diyordu — **taban altı olmak
>   silmeyi yakalatmaz**. Cümleler mekanizmaya eşitlendi: taban **bozuk taramayı**
>   korur, silmeyi **fazlalık kolu** yakalar.
>
> 🔴 **(G2) *"ARTIK HİÇ YAZILMIYOR"* DEDİĞİM SAYI SEVK EDİLİYORDU VE 9 KAT YANLIŞTI.**
> `locationsview.go` *"the largest single tenant holds **11**"* diyordu; denetçinin
> ölçümü **101**. ⚠️ Ve o sayı **yük taşıyordu**: `plaquePageLimit = 200` tavanının
> *"pratikte karşılanmıyor"* argümanının **tamamı** oydu — yani §4.6'nın *"liste
> eksik"* uyarısının teorik olduğu iddiasının. Seviye **silindi**, yerine neyin
> bozduğu (**bu görevin kendi journey testleri**) ve neyin sabit olduğu (**şekil**:
> plaket kapı pervazına vidalanır) yazıldı — `plaque.go` ile **aynı politika**, artık
> iki dosya zıt şey söylemiyor.
>
> 🔴 **(G3 + SÜPÜRME) MEKANİK KURAL UYGULANDI.** *"Kodun sahip olduğu bir kümeyi
> tarif eden çıplak bir tamsayı yorumlarda yer almaz."* Dört dizin tarandı
> (`internal/handler`, `internal/domain/tenant`, `web/templates`, `db/queries`):
> **169 aday** eşleşti, elle ayıklandı — çoğu *"iki gerçek"*, *"iki sebep"*,
> *"ikisini ayırt etmek"* gibi **küme tarif etmeyen** düzyazı, ve bir kısmı
> **şemanın** sahip olduğu sabitler (altı `ON DELETE RESTRICT` anahtarı, beş sütunluk
> GRANT, beş alanlı payload) — onlar **kalıyor**, çünkü onları büyüten şey bir
> migration ve o zaten kendi kaydını yazıyor.
> **Kod-sahipli küme tarif eden 17 yer bulundu ve hepsi kapatıldı:**
> **11 sayısız kuruldu** (*"the words below"*, *"every write route it mounts"*,
> *"the Mode values are enumerated at …"*), **4 türetime bağlandı** (eylem sözlüğü,
> rota kümesi, `plaqueActWords`, `ClaimsARemoval`/`ClaimsAPlaqueAct`), **2 tarihine
> + yönüne bağlandı** (plaket popülasyonu, ×2 dosya). Denetçinin adlandırdıklarının
> hepsi bunun içinde: `locationsview.go:180/189` · `locations.go:111/247` ·
> `plaques.go:503` · `plaque.templ:139` (*"THREE STATES AND NO FOURTH"* — altındaki
> switch'in **dört** kolu vardı ve `"replace"` diye bir mode **yoktu**) ·
> `adminlogin.go:125/132` · `db/queries/locations.sql:120` **artı iki üretilmiş
> kopyası**. Ayrıca kendi dosyalarımda denetçinin görmediği beş tane daha:
> `plaque.go:126` (*"TWO ACTIONS, NOT THREE"* — üç var), `plaque.go:1112`,
> `plaques.go:371`, `dashboard.go:82`, `locationactions.go:19`, `plaques_test.go:465`.
>
> ⚠️ **HER DÜZELTME `grep` ÇIKTISIYLA KANITLANDI** (4. turda *"düzeltildi"* dediğim
> iki paragraf düzeltilmemişti ve bunu denetçi ölçtü). Tek kalan eşleşme, retro
> olarak **alıntılanan** cümlenin kendisi — *"It said 'the largest single tenant holds
> 11'"* — ve bu bilinçli: bu repo geri çekilen iddiaları siler değil, tarihiyle
> **yazar**.
>
> **2332 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** · `-short` skip **3**
> · `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı **dokunulmamış** ·
> `make simulate-day` **PASS** · `app.css` **diff yok** · migration dizini
> **dokunulmamış**.

> **Kart düzeltmesi (2026-08-10, M6-06 B paneli — 6. TUR, SON GENEL TUR).** Denetçinin
> kapanış cümlesi: *"Hiçbir §4 kırmızı çizgi ihlali, hiçbir davranış kusuru
> bulmadım."* Üç bulgunun **üçü de ağ/metin**; ritmin durma kuralı tetiklendi.
>
> 🔴 **(H3) BİR `var` ÜÇ AĞIN DA ALTINDAN GEÇİYORDU — F1'in kendisi.** Denetçi
> `var ActionPlaqueSeized = "plaque.seized"` yazdı: **doğru önek, doğru dosya, doğru
> paket**, domain tarafından gerçekten yazılıyor, kelimesi yok → **üç test de yeşil**.
> Sebep: tarayıcı yalnız `token.CONST` okuyordu — *"hangi anahtar kelimeyi yazdığına
> bağlı bir sözlük taraması, anahtar-kelime şeklinde bir deliği olan taramadır."*
> - **Sevk edilen:** `token.VAR` eklendi; **ve kapsam ada değil DEĞERE de bağlandı** —
>   `plaque.` ile başlayan bir dize sabiti, adı ne olursa olsun toplanıyor.
> - **Ve yazma tarafındaki ağ denylist'ten allow-list'e çevrildi:** `Action:` alanı
>   artık **`Ident` ya da `SelectorExpr` olmak zorunda**; `"plaque." + "x"`
>   (BinaryExpr) eskiden geçiyordu, şimdi **KIRMIZI**.
> - **Mutasyonlar:** denetçinin `var`'ı → **KIRMIZI**; BinaryExpr yazımı → **KIRMIZI**.
> - ✅ **VE BURADA DURULDU (ikinci durma kuralı).** Kalan şekiller **ölçülerek**
>   sayıldı, tahminle değil — testin başında bir **tablo** halinde: yakalanan altı
>   şekil, yakalanmayan **üç** şekil ve her birinin **neden** yakalanmadığı. İkisi
>   zararsız olduğu **ölçüldü**: adı da değeri de eşleşmeyen bir eylem (`"plq.seized"`)
>   **bu ekrana hiç ulaşmıyor**, çünkü `ListPlaqueHistory` `action LIKE 'plaque.%'`
>   filtreliyor (iki yönde de sürüldü). Gerçekten açık olan biri: **çalışma anında
>   kurulup bir isme atanan** eylem — bugün kimse yapmıyor, kapatmak yazma anında bir
>   run-time iddiası ister, o da **başka bir mekanizma**.
> - ⚠️ **VE LİMİT LİSTESİ YANLIŞ TESTE BAĞLIYDI:** *"bunları
>   TestPlaqueActions_AreWrittenOnlyFromTheDeclaredConstants tutuyor"* diyordu; o test
>   **değerin ŞEKLİNİ** zorluyor, **adın nasıl bildirildiğini** değil. İki ağ, iki iş —
>   yazıldı.
>
> 🔴 **(H2) ONAY MATRİSİ BEŞ EYLEMİ DÖRTLE DOLAŞIYORDU — ve atlanan çift, matrisin var
> olma sebebiydi.** `plaque.unmount` ↔ `plaque.replace`, öznesi **iki tarafta da
> 14-hex plaket uid'i** olan **tek** çift: yani SUBJECT bağının, ACTION bağının işini
> kazara yapmadığı tek yer — matrisin doğduğu kusurun (*"kapı yalnızca uuid uzayları
> çakışmadığı için tutuyordu"*) birebir şekli. **Yedinci** *"elle bakılan N listesi"*
> vakası, **üstelik o sınıfı yakalamak için var olan testin içinde.**
> **Sevk edilen:** liste `deactivateconfirm.go`'nun `confirmAction*` bildirimlerinden
> **türetiliyor** (const **ve** var, okunamayan değerde **yüksek sesle düşerek**).
> Koşuda basılan: **5 eylem**, `5×5 = 25` çift, **5 köşegen**. **Mutasyon** (eylem
> bağını `parse`'tan sil) → **20 köşegen-dışı çiftin 20'si KIRMIZI**. İki bayat yorum
> (*"THE FOURTH ACTION"*, *"the three names"*) da düzeltildi.
>
> 🔴 **(H1) SÜPÜRMENİN TAMLIK İDDİASI YANLIŞTI — altı yer daha, biri düzeltmenin
> yaşadığı dosyada.** Kapatılanlar, her biri **kendi `grep`'iyle 0 eşleşmeye**
> indirildi: `plaques.go:50` (*"The two POST routes"*, altında üç) ·
> `locations.go:70` (aynı, altında dört) · `plaques.go:24` (sözlük — **UN-BIND
> yoktu**, dördüncü vaka) · `plaqueview.go:123` (*"THREE POSSIBLE ACTS"*, ardından
> iki sayıyor) · `plaqueactions.go:15` (başlık un-mount'u saymıyor, oysa **aynı
> dosyada**) · `locationsview.go:141` (*"six days later"* → **ertesi gün**;
> ⚠️ **bayat bir sayıyı geri çeken paragrafın içinde yeni bir yanlış sayı**).
> **Ve `cmd/` de süpürüldü** (`main.go` ×2).
> - ✅ **SÜPÜRMENİN SINIRI ARTIK YAZILI, çünkü yazılmamış olması bulgunun yarısıydı:**
>   taranan yer **beş dizindir** — `internal/handler`, `internal/domain/tenant`,
>   `web/templates`, `db/queries`, `cmd`. Üretilen dosyalar (`*_templ.go`,
>   `internal/store/*.sql.go`) **kaynaklarından** düzeltiliyor, elle değil. Taranmayan:
>   `docs/`, `db/migrations` (uygulanmış migration **değiştirilmez**, §3), ve M6-06
>   dışındaki paketler — oradaki bayat sayımlar **kendi görevlerinin** işi.
>
> 📌 **KURALIN ADI KONDU** (orkestratör, 2026-08-10): *"Kodun sahip olduğu bir kümeyi
> tarif eden çıplak bir tamsayı yorumlarda yer almaz."* **Şemanın** sahip olduğu
> sabitler (altı `ON DELETE RESTRICT` anahtarı, beş sütunluk GRANT, beş alanlı
> payload) **kapsam dışıdır** — onları büyüten şey bir migration, ve migration kendi
> kaydını yazar.
>
> **2332 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu** · `-short` skip **3**
> · `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı **dokunulmamış** ·
> `make simulate-day` **PASS** · `app.css` **diff yok** · migration dizini
> **dokunulmamış** · türetilen kümeler koşuda basılıyor: **7 yazma rotası**,
> **5 onay eylemi**, **3 plaket eylemi**.

> **Kart düzeltmesi (2026-08-10, M6-06 B paneli — KAPANIŞ TURU).**
> `tappa-security-auditor` **ONAY**. Bu turun üçü de ucuzdu; biri ölçüm, biri ağ,
> üçü yazı.
>
> 🔴 **(I1) *"BOUNDED"* YANLIŞ ŞEYİ SINIRLI SÖYLÜYORDU.** `ListPlaqueHistory`'nin
> `@row_limit`'i **ÇIKTIYI** sınırlıyor, **İŞİ** değil. Kendi `EXPLAIN (ANALYZE,
> `BUFFERS)`'ım (2026-08-10, demo tenant; audit_log'unda **2.566** satır / tabloda
> **63.624**, 19 MB; geçmişi **sıfır** olan bir plaket, yani hiçbir şeyin kısa devre
> yapmadığı en kötü hâl): `audit_log_tenant_at_idx` üstünde Bitmap Index Scan
> (2.566 satır, 25 buffer) → Bitmap Heap Scan, **Rows Removed by Filter: 2.566**,
> Heap Blocks exact **1.039**, **shared hit 1.064**, **4,174 ms**, **dönen satır 0**.
> Yani tenant yüklemi indeksli, `target`/`action` **değil**: tenant'ın her satırı
> okunup atılıyor. Maliyet **işletmenin yaşıyla** doğrusal, plaketin geçmişiyle değil
> — ve `audit_log` append-only, retention job yok. Cümle ölçüme eşitlendi (SQL'de ve
> domain'de), **indeks EKLENMEDİ**: o bir migration, 00013 bu fazın yuvasıydı ve
> harcandı; aday `(tenant_id, target)` ya da `(tenant_id, action, target)` ve
> **backlog'a orkestratör yazacak** (M6-07 raporları da audit indeksi isteyecek).
>
> 🔴 **(I2) BİR YORUM YARIŞI ADLANDIRIYORDU, TEST ARDIŞIKTI.** Üç yeni yazma için
> N-goroutine/`-race` testi **yoktu**. Üçü de eklendi (**10 goroutine, `-race`**):
> un-mount → **tam 1 kazanan, 9 tipli ret, 1 audit satırı** · replace → **1 kazanan,
> ve iki yedekten tam biri hâlâ kutuda** · mount → **1 kazanan**. Bedel ölçüldü:
> üçü birlikte **0,60 sn**, `make test` **171–181 sn**.
> - 🔴 **VE MUTASYON İLK ŞEKİLDE KIRMIZI VERMEDİ — bu bir ÖLÇÜM ve karta giriyor.**
>   `Unmount`'un atomik ön koşulunu *"önce SELECT et"* ile değiştirdim: test **n=10'da
>   da n=30'da da YEŞİL** kaldı. Sebep: `Unmount` yazmadan **önce satırı okuyor**
>   (iz satırı için duvara ihtiyacı var, yazma onu yok ediyor), yani READ COMMITTED
>   altında kazanan commit ettikten sonra başlayan goroutine **o okumada** reddediliyor
>   ve UPDATE'e hiç varmıyor. Bu, ifadenin `WHERE`'ini süs yapmaz — **pencere içinde
>   tek muhafız odur** — ama un-mount yarışı **SONUCU** kanıtlıyor, **MEKANİZMAYI**
>   değil.
> - ✅ **Mekanizmayı pinleyen ayrı bir test yazıldı: MOUNT'un ön okuması YOK.**
>   `AssignTagToLocation` eylemin tamamı, her goroutine UPDATE'e varıyor, karar
>   ifadenin `status = 'unassigned'`'ına kalıyor. Aynı mutasyon orada
>   **deterministik KIRMIZI** (*"10 of 10 mounts succeeded"*).
>
> 🟡 **(I3) ROTA TÜRETİMİNİN ÖNEK SINIRI YAZILDI (kapatılmadı — ikinci durma
> kuralı).** Başka bir önekte mount edilecek bir plaket yazması ağdan kaçar. Her
> POST'a genişletmek `/admin/login` ve `/admin/logout`'u içeri alır ve **rota başına
> muafiyet listesi** gerektirir — bu görevde altı bulgunun konusu olan şeklin ta
> kendisi. Sınırı yaşanabilir kılan şey yazıldı: bu bölümün **yedi href'inin yedisi
> de** `locationsHref`'ten kuruluyor (grep'lendi), yani önekten çıkarmak testin kendi
> paketindeki **görünür bir düzenleme**.
>
> 🟡 **(I4, yalnız yazıldı) ONAY JETONU HALEFE BAĞLI DEĞİL.** *"X'i değiştir"* onayı
> X'i **herhangi bir** yedekle değiştirmeye yetiyor. Mekanizmanın beyan ettiği şeyle
> (uyarı **X için** gösterildi mi) tutarlı, uyarı zaten X hakkında, hangi yedeğin
> çıkacağı **aynı ekranda müdürün kendi seçimi**, ve halef yine doğrulanıyor +
> aynı tenant + `unassigned`. Bağlamak payload'a ikinci bir özne (v4) koymayı
> gerektirir ve bu kapının karşı karşıya olduğu aktöre karşı **hiçbir şey**
> kazandırmaz. `plaqueactions.go`'ya yazıldı.
>
> 🟡 **(I5, yalnız yazıldı) T7'YE YENİ BİR OLGU — ve §4.6 kaybı.** `aes_key_ref`'i
> 2 baytlık bir satır kartta **"Encoded by Tappa"** gösteriyor; o plakete NFC tap →
> **500** ve `transactions`'a **hiçbir satır yazılmıyor** (sayaç ilerlemiyor). Yani
> gap yalnız kartta yanlış bir kelime değil, **tap yolunda kaydın kaybolması**.
> **Panelin kusuru değil** (panel öyle bir satır yaratamaz, `tags`'a INSERT yok, ve
> onaramaz), ama yanıltıcı kelimenin **üretildiği yere** — `keyStateOf`'un yanına —
> ölçümüyle yazıldı ki T7'yi kapatan kişi maliyetin tamamını tek yerde bulsun.
>
> ⚠️ **VE BAŞKA BİR GÖREVİN TELİ BENİM DÜZYAZIMI İKİ KEZ YAKALADI.** M6-05'in
> `TestComments_DoNotQuoteTheDriftingRosterSize` teli, *"tenant"* kelimesinin hemen
> ardından parantez içinde dört haneli bir sayı gelmesini **rezerve ediyor** — çünkü
> bu, sürüklenen bir kadro sayısının aldığı şekil. I1'in ölçümünü önce **kodda**, sonra
> **bu kartta** tam o şekilde yazdım; bütçe iki kez **6 → 7** oldu.
> - **Doğru cevap teli gevşetmek değildi:** M6-05 bir genişletmeyi **30 meşru ölçümü**
>   işaretlediği için geri almıştı, ve *"meşru düzyazıda ateşleyen bir tel, bir
>   sonrakinin sildiği teldir"* o dosyanın kendi cümlesi.
> - **İkincisi daha öğretici:** yasak şekli **ALINTILAYARAK** açıklamak da onu taşır —
>   `employees_test.go`'nun kendini taramadan muaf tutmasının sebebi bu, ve skill
>   `tappa-brand`'in Tailwind sınıfları için yazdığı kural aynı: **şekli tarif et,
>   yazma.** Bu paragraf artık öyle yazılmıştır.
> - Bütçe **6**'ya döndü ve bu turun eklediği **hiçbir dosya** rezerve şekillerin
>   hiçbirini taşımıyor (kendi tarayıcımla, testin kendi regexp'iyle kontrol edildi).
>
> **2335 PASS / 0 FAIL / 0 SKIP · 16 paket · iki ardışık koşu (171 / 181 sn)** ·
> `-short` skip **3** · `fmt`/`gen`/`lint`/`audit` **exit 0**, tarayıcı
> **dokunulmamış** · `make simulate-day` **PASS** · `app.css` **diff yok** ·
> migration dizini **dokunulmamış** · `advanceTagCounter` sabiti **HEAD ile bayt bayt
> aynı** (359 B, sha256 `bfb75e9d…`) ve `AdvanceTagCounter` gövdesi de.

---

## M6-07 — Reports ve CSV export

- **Bağımlılık:** M6-03 · M4-05
- **Commit (FAZ A):** `feat(dashboard): add worked-hours reports`
- **Commit (FAZ B):** `feat(dashboard): add csv export`

> ⚠️ Tek satırlık `feat(dashboard): add reports and csv export` commit'i **kaldırıldı**
> (2026-08-10): görev iki faza bölündü ve A fazı dürüstçe *"and csv export"* diyemez —
> CSV'nin tek satırı yazılmadı. İki satır, iki tur, iki commit.

> **Kart düzeltmesi (2026-08-10, M6-07 A uygulaması sırasında).** Görev **iki faza
> bölündü** (orkestratör kararı). Ölçüt kapsam değil **denetim merceği**: A'nın
> merceği *aritmetik doğruluk · §6 float yok · §4.5 tenant · §4.7 ne seçilmiyor ·
> §5'in rapor cümleleri*; B'ninki *çıktı yüzeyi · enjeksiyon · toplu veri çıkışı ·
> `audit_log`*. Aynı turda bakılsalardı ikisi de yarım bakılırdı — M6-01/M6-06'da
> üç parçada da böyle oldu.
>
> - **FAZ A (bu tur, kapandı):** çalışılan saat motoru (`internal/domain/ledger/
>   report.go`) + `/admin/reports` ekranı + sorgular
>   (`ListWorkedShiftEvents`, `CountPracticeTaps`).
> - **FAZ B (ayrı tur):** CSV export.
>
> **A'nın B'ye devrettiği yükümlülükler — B bunları KAPATMADAN kapanmaz:**
> 1. **Aritmetiği yeniden yazma.** `ledger.Reader.Hours` bir `Report` döndürür ve
>    ekran onu yalnızca *biçimlendirir*. CSV aynı `Report`'tan üretilmelidir; ikinci
>    bir toplama kodu = bu repoda beş kez bedeli ödenmiş "ikinci temsil" sınıfı.
> 2. **CSV kaçışı (`= + - @`).** templ kaçışı CSV'ye **geçmez**. Çalışan adı, mekân
>    adı ve `Unknown employee` gibi bizim ürettiğimiz metinler de dahil her hücre.
>    Kartın 2268. satırındaki uyarı buraya aittir.
> 3. **§4.7 — sorgu zaten koordinat SEÇMİYOR, ama B yeni sütun EKLEMEMELİ.**
>    `gps_lat/gps_lng/source_ip/policy_context/entered_by` CSV'ye "raporu
>    zenginleştirmek için" eklenmeye en açık yerdir. Eklenirse §4.7 duvarı düşer.
> 4. **`audit_log` kaydı.** Toplu veri çıkışı bir olaydır: kim, ne zaman, hangi
>    hafta, kaç satır. A hiçbir şey yazmıyor; B'nin **tek yazışı** budur ve
>    `ProtectWriting` zinciri gerekip gerekmediği B'nin kararıdır (indirme GET mi
>    POST mu — GET ise `audit_log` yine yazılır).
> 5. **Yetkilendirme.** `report:export` politikası M6-09'un ekranında adı geçiyor
>    ama panelde `policy.Evaluate` çağıran **hiçbir handler yok** (ölçüldü). B ya
>    bu politikayı gerçekten okur ya da okumadığını **açıkça yazar**.
> 6. **UTC + yerel, ISO 8601, başlıkta hangisi olduğu açık** — kartın kendi
>    kriteri. A ekranda yalnız **yerel** gösteriyor ve zaman dilimini yazıyor;
>    CSV'de ikisi de olacak.
> 7. **Kesilmiş okuma (`ledger.ReportEventCap`) CSV'de de görünmeli.** A ekranda
>    "bu toplamlar eksik" diyor; sessizce eksik bir CSV **bordroyu eksik çıkarır**.
> 8. **Sayfalama sınırı CSV'de YOK olmalı.** `maxReportRows` yalnız *listeyi*
>    kısıtlıyor (toplamlar herkesi kapsıyor); CSV'nin var olma sebebi kadrosu
>    200'den büyük işletmedir.
>
> **A'nın verdiği kararlar** (gerekçeleri kodda, ilgili yorumlarda):
> gece vardiyası **başladığı güne** sayılır (bölünmez) · hafta **pazartesi**
> başlar (ISO 8601, iki pazar da öyle) · gün sınırı **yerel** · bekleyen `flag`
> saate **girmez** ama ayrı sayıyla gösterilir, **onaylanmış** `flag` girer,
> **reddedilmiş** `flag` girmez ve ayrı gösterilir · `closingTail = tap.StaleOpenIn`
> (18 sa) hem okuma ufku hem "bu bir vardiya değil" eşiği.

> **Kart düzeltmesi (2026-08-10, M6-07 B uygulaması sırasında).**
>
> **SEKİZ YÜKÜMLÜLÜĞÜN KAPANIŞI.** Hepsi `internal/handler/reportscsv.go` +
> `reportscsv_test.go` + `reportscsv_db_test.go` içinde, her biri **mutasyonla**
> kırmızıya döndürülerek:
> **(1)** CSV aynı `ledger.Report`'tan üretiliyor, ikinci toplama yok —
> `TestReportCSV_AgreesWithTheScreenItWasExportedFrom`. **(2)** Kaçış `= + - @`
> (aşağıya bak). **(3)** Yeni sorgu/sütun **yok**; CSV sütun adları da `entered_by`
> dahil beş şema adına karşı taranıyor. **(4)** `audit_log`'a `report.exported`,
> **bölümün tek yazışı**. **(5)** `report:export` **bağlanmadı** ama gerekçesi artık
> **ölçülüyor** (aşağıya bak). **(6)** Her an hem yerel hem UTC, ISO 8601, sütun adı
> hangisi olduğunu söylüyor. **(7)** `Truncated` → dosyanın **ilk** bloğunda
> `Read complete,NO` + *"EVERY FIGURE IN THIS FILE IS A FLOOR"*. **(8)**
> `maxReportRows`/`maxOpenRows` CSV'ye **girmiyor** — gerçek indirmede ekran
> **100/754**, dosya **754** satır.
>
> 🔴 **KAÇIŞTA KENDİ AĞIMI YENDİM VE ÜRÜNDE GERÇEK BİR AÇIK BULDUM.** İlk sürüm
> OWASP'ın altı tetikleyicisini (`= + - @` + TAB + CR) `switch`'e yazmıştı. Mutasyon
> ölçtü: **TAB ve CR o listede ÖLÜ** — `unicode.IsSpace` ikisi için de doğru, yani
> döngü zaten üzerlerinden atlayıp `=`'i buluyor; onları silmek **hiçbir cevabı
> değiştirmedi** ve test yeşil kaldı. Aynı ölçüm gerçek açığı gösterdi: **boşluk
> OLMAYAN kontrol karakteri** — `unicode.IsSpace('\x01')` **false**, yani
> `"\x01=1+1"` **kaçırılmadan** dosyaya yazılıyordu. Düzeltme: tetikleyici **dört**
> karakter, atlama kümesi **`IsSpace || IsControl`**. Testin kendi süpürgesi de
> hatalıydı — yalnız `rune[0]`'a bakıyordu, yani `" =1+1"`'i de göremezdi; artık
> **önce soyup sonra bakıyor**.
>
> 🔴 **`transaction_reviews.note` TAHMİNİ YANLIŞLANDI.** M6-04'ün 2. tur düzeltme
> bloğu (kartın ~1774. satırı) *"ilk tüketici muhtemelen M6-07 CSV'si"* diyordu.
> Ölçüldü: A'nın `ledger.Report`'unda not **yok**, B de **eklemedi** (yeni sütun =
> yükümlülük 3 ihlali). Notun bugünkü tek tüketicisi **Transactions docket'i**;
> CSV değil. Uyarının kendisi (`= + - @` kaçışı CSV'nin işidir) **doğruydu ve
> uygulandı**.
>
> ⚠️ **"MONO HİZALI BAŞLIK" KRİTERİ BİR CSV'DE KARŞILIĞI OLMAYAN BİR CÜMLEDİR.** Bir
> CSV yazı tipi taşımaz; §9'un mono kuralı **ekranın** kuralıdır. Karşılığı olarak
> uygulanan şey: her süre **iki sütun** (`8h 30m` + `510` tam sayı dakika), her an
> ISO 8601, her sayı tam sayı — yani "veri hücresi hizalanabilir" niyetinin CSV'deki
> gerçek biçimi. Ekranda ise indirme kontrolü markanın kendi butonudur.
>
> **KARARLAR (üçü de ölçümle, otonom).**
> - **GET, POST değil.** İki zincir ölçüldü: `Protect()` = `floodGate → requireAdmin
>   → sessionGate`, `ProtectWriting()` bunlara **`sameOriginGate`** ekliyor. Yani
>   **adres kalkanı GET'i de kapsıyor** — panelin en ağır okuması korumasız değil.
>   `sameOriginGate` eklenmedi çünkü **katı**: `Origin` yoksa `Sec-Fetch-Site`'a
>   düşüyor, o da yoksa **reddediyor** — ve tarayıcı düz bir gezinmede `Origin`
>   göndermez, yani indirme bağlantısı eski tarayıcılarda tamamen kırılırdı.
>   **SAYILMIŞ AÇIK:** çapraz-origin bir üst-düzey gezinme bir `audit_log` satırı
>   yazdırabilir (T14 sınıfı). Sınırları: çerez **SameSite=Lax** olduğu için
>   `img`/`iframe`/`fetch` **çerezsiz** gelir (yalnız üst-düzey gezinme sayılır,
>   o da kurbanın sekmesini götürür), `floodGate`+`sessionGate` onu da ücretlendirir,
>   ve satır **aktörü adlandırır**. `TestReportsExport_InheritsTheREADChainAndTheGapIsCOUNTED`
>   bunu **iki yönlü** pinliyor.
> - **UTF-8 BOM: VAR.** Ölçüldü — Go `encoding/csv` ve python `csv`(utf-8) BOM'u
>   **ilk hücreye taşıyor**, python `utf-8-sig` temiz, `cut|od` baytları gösteriyor.
>   Karşı taraf: BOM'suz UTF-8 CSV Excel'de sistem kod sayfasıyla açılır ve
>   **ħ ġ ċ ż / ı ş ğ ç** bozulur — yani **Malta ve Türkiye**, yani ürünün iki pazarı.
>   Bir **isim** sessizce bozulur, BOM ise **tek bir hücreye** üç bayt ekler; bu
>   yüzden dosyanın **ilk satırı bilinçli olarak bir başlık cümlesi**, sütun adı
>   değil — her sütun başlığı satırı boş satırdan sonra başlıyor ve **temiz**.
>   `TestReportCSV_PutsTheBOMWhereNoColumnHeadingIs` yerleşimi pinliyor.
> - **Audit `Record` ile, `RecordTx` ile değil.** `Reader.Hours` bir **okuma**dır ve
>   kendi transaction'ını açıp kapatır; bir okuyucudan `pgx.Tx` sızdırmak okuma
>   yolunu yazma yoluna çevirirdi. Audit yazımı **indirmeyi reddettirmiyor**, ve
>   gerekçe ölçüm: **aynı sayılar `GET /admin/reports`'ta zaten audit'siz
>   erişilebilir** — yani satır *"dosya üretildi"* kaydıdır, veriye açılan kapı değil.
> - **`report:export` motora BAĞLANMADI (orkestratör kararı) ve gerekçe artık
>   ölçülüyor:** `TestReportExportAuthority_IsGrantedToBothPanelRolesByTheBaseline`
>   baseline'ı okuyup eylemin **owner VE manager**'a verildiğini kanıtlıyor (rol
>   kapısı bugün kimseyi reddetmez), `TestPanel_CallsThePolicyEngineNowhere` paketi
>   `go/ast` ile tarayıp `policy.Evaluate` sayısının **0** olduğunu kanıtlıyor.
>   Baseline daralırsa **test kırmızıya döner** ve karar yeniden alınır. Gerçek kapı
>   **M6-09**.
> - **Dosya adında tenant adı YOK.** `Content-Disposition` değeri tırnaklı ve tenant
>   adı serbest metin; ad yalnız **sunucunun** ürettiği ISO günden kuruluyor ve bir
>   `^\d{4}-\d{2}-\d{2}$` kalıbıyla doğrulanıyor (uymayan her şey sabit ada düşer).
>   Go'nun `net/http`'si CR/LF'i zaten boşluğa çeviriyor; **tırnağı çevirmiyor**, ki
>   asıl risk oydu.
>
> **ÖLÇÜMLER (2026-08-10, dev DB, gerçek `curl -b jar` oturumu).** Ekran **100/754
> kişi**, CSV **754**; ikisi de **`4h 32m`** diyor. CSV **114 937 B** / ekran
> **114 175 B**; istek süresi CSV **0,100–0,136 s**, ekran **0,099–0,127 s** (3'er
> tekrar) — yani CSV **7,5 kat satırı** ayırt edilemez maliyetle taşıyor. Satır
> başına: kişi **~125 B**, açık giriş **~82 B**; tavan `ReportEventCap / 2` kişi =
> **1 252 537 B**, **35 ms**'de üretiliyor — bu yüzden tamponlamak (ve
> `Content-Length` vermek) karşılanabilir. **Yeni migration yok, yeni sorgu yok**,
> dolayısıyla `EXPLAIN` gerekmedi.
>
> ⚠️ **DEV VERİTABANINDA BIRAKILAN İZ:** kaçış kanıtı için `curl` ile indirilebilen
> bir **atılabilir tenant** oluşturuldu (`aaaaaaaa-0000-4000-8000-00000000c5a1`, adı
> bir `=HYPERLINK(...)` formülü, kendi owner'ı var, parolası seed'in belgeli dev
> parolası). `transactions` **§4.3 gereği silinmedi** — sessizce temizlemek yerine
> yazılıyor. Aynı sınıfın otomatik karşılığı `TestPanelReportsCSVDB_EscapesAFormulaThatCameOUTOfPostgres`.

> **Kart düzeltmesi 2 (2026-08-10, M6-07 B düzeltme turu — iki denetçi ONAY, altı
> bloklamayan bulgu).**
>
> 🔴 **KAÇIŞ AĞI BİR SINIF DAHA GENİŞ YANLIŞTI VE ÇÖZÜMÜ REPODA HAZIR DURUYORDU.**
> Bir önceki düzeltmede atlama kümesi `unicode.IsSpace || unicode.IsControl` yazılmıştı.
> Go'da **`IsControl` yalnız `Cc`'dir** — yani **`Cf` (format) kategorisi tamamen
> dışarıda kaldı**. Uçtan uca ölçüldü (çalışan adı olarak seed → gerçek handler →
> gerçek HTTP): `U+FEFF U+200B U+200C U+200D U+00AD U+2060 U+202D U+202E U+200F
> U+2066 U+2069 U+0600 U+180E` — **on üç rune canlı bir formülün önünde kaçışsız**
> dosyaya ulaşıyordu. Ve `internal/session/manager.go` **aynı sınıfı zaten ayrı
> `case` olarak çözmüş** ve kendi doc yorumunda formül nötrleştirmeyi **adıyla bu
> göreve** devrediyor. Yani bu, *"kalıbın yarısını kopyalama"* sınıfının **beşinci**
> vakası — ve diğer yarısı **bir dosya ötedeydi**.
> `Mn`/`Me` de ölçüldü: `U+034F`, `U+0301`, `U+17B4` de kaçışsız geçiyordu; **atlama
> kümesini genişletmek yanlış alarm ÜRETEMEZ** (atlama tek başına asla kaçış yapmaz,
> yalnız bakmaya devam eder) — testin "sıradan hücre" listesi bunu kanıtlıyor.
> `Mc` **bilinçli dışarıda**: boşluk kaplayan bir işaret saklanma yeri değildir.
> ⚠️ **`U+FF1D` (tam genişlik ＝) ölçülemedi** — makinede hesap tablosu motoru yok, iki
> denetçi de aynı duvara çarptı. **Karar: tetikleyici listesine eklendi** (dört
> karakterin tam genişlik ikizleri + `U+2212`), gerekçe **hata maliyetinin
> asimetrisi**: yanlışsa maliyet zaten metin olan bir hücreye bir apostrof, doğruysa
> maliyet kod çalıştırma. **Sayılmış limit artık RAPORDA DEĞİL `startsAFormula`'nın
> kendi yorumunda** — bir önceki turda bunu yalnız rapora yazmıştım, ki *"raporda
> saymak kodda saymak değildir"*.
>
> 🔴 **İKİNCİ KAYIPLI DÖNÜŞÜM: TEK BAŞINA CR SESSİZCE DÜŞÜYOR.** Yorum apostrofu
> **tek** kayıplı adım gibi sunuyordu. Ölçüldü: `csv.Writer{UseCRLF:true}` CRLF'in
> parçası olmayan bir CR'yi **siliyor** (`"Maria\rBorg"` → `"MariaBorg"`), `\r\n` de
> okumada `\n`'e katlanıyor; `full_name` sınırsız `text`, CHECK yok, yani değer
> **saklanabilir**. **Davranış değiştirilmedi ve gerekçesi yazıldı:** `UseCRLF=false`
> CR'yi korur (ölçüldü) ama satır sonlarını LF yapar **ve tırnaklı alanın içine çıplak
> bir CR koyar** — hoşgörülü bir ayrıştırıcının kaydı ikiye böldüğü tam o bayt. Kaydın
> kendisi §4.3 gereği Postgres'te **aynen duruyor**; kaybolan şey **render**.
> `TestReportCSV_LosesABareCarriageReturnAndSaysSo` pinliyor.
>
> **AUDIT GEREKÇESİ DARALTILDI.** *"Aynı sayılar zaten audit'siz erişilebilir"*
> **toplamlar için doğru, satırlar için değil**: `maxReportRows`/`maxOpenRows` 100 ve
> **ekranda kursör yok** (`reports.go` kendi ağzıyla *"this page cannot page"*), yani
> **101. kişinin satırı yalnızca export'tan** alınabiliyor. Kalan nüans da yazıldı:
> fark **hesap verebilirlik**, gizlilik değil — altta yatan kayıtlar sayfalanabilir
> Transactions'tan okunabiliyor ve **o da audit yazmıyor**, ve §4.6 tap kaydıyla
> ilgilidir, erişim iziyle değil. Daraltılmış cümlenin dayandığı olgu
> `TestReportsScreen_HasNoCursorSoRowsPastTheCapAreEXPORTOnly` ile pinlendi.
>
> **EKRAN/DOSYA ASİMETRİSİ KAPATILDI.** Ekran `needsActionLine` ile *"kaçı çok uzundur
> açık"* **toplamını** basıyordu, CSV'de yalnız satır başına `needs_action` vardı.
> Artık dışlama bloğunda **`Open too long to be somebody on shift`** satırı var,
> aynı `OpenEntry.Stale` alanından sayılıyor (ikinci eşik yok).
>
> 🔴 **E5 — İKİ DENETÇİNİN ÇELİŞKİSİ ÇÖZÜLDÜ: GÜVENLİK MERCEĞİ HAKLI.**
> `ListWorkedShiftEvents`'in **gerçek `WHERE`'i** birebir uygulanarak, `ANALYZE
> transactions` sonrası, en yoğun tenant/hafta (2026-08-10):
>
> | yüklem | satır | tavanın |
> |---|---|---|
> | practice filtresi yok, kuyruk yok (**naif şekil**) | 19 134 | %96 |
> | practice filtresi yok, kuyruk var | 22 144 | **%111** |
> | `NOT practice`, kuyruk yok | 10 839 | %54 |
> | `NOT practice`, kuyruk var (**BU SORGU**) | **12 562** | **%63** |
>
> **Farkın tamamı `NOT t.practice`:** tabloda `type IS NOT NULL` **152 519** satır,
> `+ NOT practice` **104 353** — yani filtreyi atlayan bir sayım haftayı **yarı yarıya
> şişiriyor**. 18 saatlik kuyruk ters yöne çalışıyor ve çok daha küçük. Genel gözün
> **1,045×**'i bir **popülasyon etiketi hatası** (practice tap'leri sayıyor);
> **orkestratörün 1,64×'i ise YANLIŞ DEĞİL, BAYAT** — 20 000/12 193 aynı (B)
> popülasyonundan geliyor, bugün 20 000/12 562 = **1,59×**. Doğru cümle: tavan en
> yoğun ölçülen haftada bütçesinin **yaklaşık üçte ikisinde**, eşikte değil. Ölçüm ve
> dört varyantın sorgusu `internal/domain/ledger/report.go`'daki `ReportEventCap`
> bloğunda.
>
> ⚠️ **İKİ BAYAT ÖLÇÜM CÜMLESİ DÜZELTİLDİ.** (a) Bayt tablosu **fixture'ını
> adlandırmıyordu** ve satır başına bayt **isim uzunluğunun doğrudan fonksiyonu** —
> denetçi kısa isimle **1 102 537 B** ölçtü, tabloda **1 252 537 B** yazıyordu; fark
> tam olarak isim uzunluğu farkı, yani **denetçi haklı**. Artık fixture adlandırılıyor
> ve maliyet **formülle** yazılıyor (**103 B yapı + isim**),
> `TestReportCSVSize_IsAboutTheNameLengthPlusAFixedRow` her koşuda yeniden türetiyor.
> (b) *"754 ← dev DB'nin kendi haftası"* günler içinde **814** oldu; üç haneli sayılar
> tripwire'ın `\d[ ,.]?\d{3}` kalıbına **takılmıyor**, yani kimse yakalamazdı. Mutlak
> sayı **silindi**, yerine oran kondu.
>
> **MUTASYONLAR (hepsi bu turda, uygulandığı ve geri alındığı `git diff --no-index`
> ile gösterildi):** `Cf`'yi atlama kümesinden düş → **KIRMIZI** (birim **ve** DB
> testi) · kümeyi `IsSpace||IsControl`'e geri al → **KIRMIZI** · `Mn`/`Me`'yi düş →
> **KIRMIZI** · tam genişlik ikizlerini düş → **KIRMIZI** · `UseCRLF=false` →
> **KIRMIZI** · needs-action satırını sil → **KIRMIZI** · sayacı ekran tavanıyla
> sınırla → **KIRMIZI** · isim sütununu kırp → **KIRMIZI**.
>
> ⚠️ **KENDİ HATAM (yine iki tane):** needs-action fixture'ımın ilk hâli **ayırt edici
> değildi** — bayat girişlerin hepsi ilk sayfadaydı, yani sayacı sayfayla sınırlayan
> mutasyon **yeşil kaldı**; taze girişler öne alındı ve *"sayfa sınırlı bir sayaç en
> fazla kaçı görebilir"* koruması eklendi. Ve bayt testinin ilk türetmesi **boş isimli**
> bir rapordan çıkarıyordu — oysa `personName("")` **"Unknown employee"**, yani boş
> fixture bu ölçümün sıfırı değil; türetme **aynı isim uzunluğunda iki satır sayısına**
> çevrildi.

> **Kart düzeltmesi 3 (2026-08-10, M6-07 B son tur — üç onay, altı metin/kapsam bulgusu).**
>
> 🔴 **"HER BİRİ TEK BAŞINA KIRMIZIYA DÖNDÜ" İKİ ÖĞE İÇİN YANLIŞTI, VE İDDİA ARTIK
> TÜMÜYLE ÖLÇÜLÜ.** Denetçi `Me`'nin ve `U+FF0D`'nin **tek başına** düşürülünce yeşil
> kaldığını ölçtü — kardeşleri vakayı taşıyordu. Doğrulandı ve kapatıldı: her ikisine
> **kendi test vakası** eklendi (`U+20E0`, `U+FF0D`). Sonra **her tetikleyici ve her
> atlama sınıfı tek tek** düşürüldü: **26/26 KIRMIZI** (19 tetikleyici + 7 atlama).
> ⚠️ **Harness'ımın ilk hâli kendi tuzağına düştü:** `unicode.IsSpace` mutasyonu
> **kodu değil YORUMU** değiştirmişti (dosya iddiayı bir yorumda alıntılıyor), `git
> diff` boş olmadığı için "uygulandı" göründü ve **YEŞİL** raporladı. Harness'a
> *"fonksiyon GÖVDESİ değişmeli"* koruması eklendi; düzeltince KIRMIZI.
>
> 🔴 **SINIR ARTIK İLKELİ — VE İLKEYİ Go'NUN KENDİSİ VERİYOR.** Denetçi haklıydı:
> *"sınır bir ilke değil, aramanın durduğu yer"*. `U+3164` HANGUL FILLER kategori
> **`Lo`** — sıradan bir **harf** — ve çoğu fontta **hiç görünmüyor**; `Cc/Cf/M`
> üzerine kurulu bir sınıflandırma onu göremez. Çözüm: **`unicode.Properties
> ["Other_Default_Ignorable_Code_Point"]`** — Unicode'un *"render eden hiçbir şey
> göstermeyebilir"* özelliğinin ta kendisi, ve Go **ship ediyor**. Dört Hangul
> filler'ın dördünü de kapsıyor.
> Ayrıca kapatılanlar: **`Mc`** (artık `unicode.M` — *bir birleşen işaret bir değeri
> BAŞLATAMAZ, bir tabana tutunur*; eski *"boşluk kaplar"* gerekçesi **`Mc`+`Cf`
> zinciriyle çürütülmüştü**) · **geçersiz UTF-8** (`range` `U+FFFD` veriyor → atlama
> kümesine) · **`U+2800` braille blank** (adlandırılmış **istisna**, sınıf değil, ve
> koda öyle yazıldı) · **uyumluluk ikizleri tetikleyiciye**: `=` `+` `-` `@`'in tam
> genişlik + small + superscript + subscript formları — **NFKC katlamasının kapalı
> kümesi**, elle yazıldı çünkü `golang.org/x/text` bu repoda bağımlılık değil.
> ⚠️ **`Variation_Selector` EKLENDİ, ÖLÇÜLDÜ VE GERİ ÇIKARILDI:** tüm kod uzayında
> **münhasır rune sayısı 0** — hepsi `unicode.M` veya ODI tarafından zaten yakalanıyor.
> Tab/CR ile aynı ölü-dal şekli; bu kez **koda girmeden** yakalandı. Diğer limbler:
> IsSpace **19**, Cc **59**, Cf **170**, ODI **3 773**, M **2 187**, ikisi **1'er**.
> 🔴 **YANLIŞ ALARM MALİYETİ ÖLÇÜLDÜ:** dev DB'de CSV hücresine ulaşabilen **662 873**
> serbest metin değeri (çalışan · lokasyon · departman · tenant adları) — genişletilmiş
> kümeler **tam olarak aynı 74 değeri** kaçırıyor, **delta 0**; 30 dilli elle kurulmuş
> kontrol (Latin · Malta · Türkçe · CJK · Kiril · Yunan · İbrani · Arap · Devanagari ·
> Tay) **0** kaçış.
> ⚠️ **VE AÇIK KALANLAR KODA SAYILDI:** fontta görünmez ama default-ignorable/mark/boşluk
> **olmayan** rune'lar (`U+2800` bulunanı; property yok, çünkü *"hiç render olmuyor"*
> bir font sorusu) · NFKC dışı homoglyphler · **East Asian Width hiç bakılmıyor**
> (Go tablosu yok). ⚠️ **Ve sömürünün yarısı hâlâ ÖLÇÜLEMEZ**: hesap tablosunun bu
> tetikleyicileri gerçekten **değerlendirip değerlendirmediği** ölçülmedi (makinede motor
> yok, **üç bağımsız denetim aynı duvara çarptı**) — yazılanlar *"olabilir"* üzerine,
> **hata maliyetinin ucuz olduğu yönde** uygulanmış akıl yürütme; sömürülebilirlik
> **iddia edilmiyor**.
>
> 🔴 **YENİ BİR İKİNCİ TEMSİL BULUNDU VE KALDIRILDI (kod değişikliği).** `needsActionTotal`
> (CSV) ile `fillReportsView`'ın döngüsü **aynı slice üzerinde aynı sayıyı** ayrı ayrı
> topluyordu — dosyanın **kendi başlığının yasakladığı** şey. Denetçi bedelini ölçtü:
> CSV kopyasını `!o.Stale` yapmak **mutabakat testini yeşil** bırakıyordu. **Tek hesap
> noktası bırakıldı** (`reports.go` artık `needsActionTotal`'ı çağırıyor) — ve bunun
> yan etkisi olarak açık-giriş döngüsü **erken durabiliyor** (`continue` → `break`),
> çünkü onu sonuna kadar koşturmayı savunan gerekçe **sayacın kendisiydi**. Mutabakat
> testi bu niceliği de kapsıyor. ⚠️ **Ne yakalayıp ne yakalayamadığı yazıldı:** tek
> hesap noktasıyla mutabakat testi **yanlış cevabı** yakalayamaz (iki yüzey de aynı
> yanlışı basar) — onu **değer testleri** yakalıyor; mutabakat testinin işi **yeniden
> ikileşmeyi** yakalamak.
>
> **İKİ BAYAT ÖLÇÜM CÜMLESİ DAHA.** (a) Fixture adı **21 değil 22 bayt**
> (`Employee Number 000001`) — ve fark önemli, çünkü tablo **ancak 22 ile denkleşiyor**:
> `103 + 22 = 125`. (b) CR tablosunun üçüncü satırı *"quoted, unchanged"* diyordu;
> ölçüldü: tek başına LF **CRLF'e dönüyor**, yani **baytlar değişiyor**, korunan şey
> **round-trip**. *"Preserved"* ile *"unchanged"* ayrı iddialar.
>
> ⚠️ **KAYAN SAYI UYARAN PARAGRAFIN KENDİSİ KAYAN SAYI TAŞIYORDU** — bu paragrafın
> **ikinci** kez düzeltilmesi. `~114 KB` ve `~0,1 s` ikisi de aynı kayan kişi sayısının
> doğrusal fonksiyonuydu. Artık **formül**: `bayt ≈ kişi × (103 + ortalama ad) + açık ×
> 82 + preamble`; girdilerin ikisi de **işletmenin** özelliği, bu deponun test
> kalıntısının değil. Geriye yalnız **oran** kaldı: o hafta ekran `maxReportRows` kişi
> listeliyor, dosya **hepsini**, ve iki istek birlikte ölçüldüğünde **ayrışmıyor**.
>
> **MUTASYONLAR:** 19 tetikleyici + 7 atlama sınıfı **tek tek** → **26/26 KIRMIZI** ·
> paylaşılan sayacı ters çevir → **3 test KIRMIZI** · ekranı yeniden ikileştir →
> **mutabakat testi KIRMIZI** · isim sütununu kırp → **KIRMIZI**. Hepsi
> `git diff --no-index` ile uygulandı + geri alındı (**CLEAN**).

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

> **Kart düzeltmesi (2026-08-10, M6-08 uygulaması sırasında).** Kart **beş satırdı**
> ve altındaki iş beş satırdan büyüktü; aşağıdakilerin hiçbiri kriterlerde yazmıyordu.
>
> 🔴 **(1) `sun_valid = false` KRİTERİ YANLIŞ — `NULL` yazılıyor, ve gerekçe kartın
> kendi şemasında.** Migration 00005 `sun_valid`'i **üç durumlu** tutuyor ve üçüncüsü
> tam olarak bu vaka: *"NULL = bu kanal için değerlendirilmedi"*. `false` yazmak
> *"çipi kontrol ettik ve tutmadı"* demek olurdu — hiç yapılmamış bir kontrol hakkında.
> `internal/domain/checkin`'in `insertParams`'ı **zaten** manual kanalda `sun_valid`
> yazmayı reddediyor (`if req.Channel != tap.ChannelManual`), yani kart kodun
> bugünkü davranışıyla da çelişiyordu. Sevk edilen: sütun **INSERT'ün sütun
> listesinde bile yok**.
>
> 🔴 **(2) VERDICT KRİTERDE HİÇ YOK, VE EN TARTIŞMALI KARAR O.** İki okuma ölçüldü:
> - `flag` — §4.6 *"kanıt yetersizse flag"* der. Ölçüldü: bir `flag` manuel kayıt
>   `endpointState` (M6-07) tarafından **`HoursAwaiting`** sayılır, yani saat
>   toplamına **girmez**; müdür unutulmuş çıkışı kapatır ve **toplam hâlâ eksik
>   kalır** — Q18'in var olma sebebinin tam tersi. Onay kuyruğunda sonsuza kadar
>   oturur (onaylayacak kişi yazan kişidir) ve verilen onay **geri alınamaz**
>   (ADR 0009).
> - `ok` — Q18 *"kayıt her zaman bir insanın beyanına dayanır"* der ve beyan
>   **kanıtın kendisidir**, `entered_by` ile hesap verebilir birine bağlı.
>   **Seçilen budur.** Kaydı dürüst kılan şey daha kötü bir verdict değil,
>   **kanaldır** — her rapor onu zaten ayırıyor.
> - **Verdict Go'da değil SQL'de sabit:** `InsertManualTransaction` `'ok'` literalini
>   yazıyor, parametresi yok. Mutasyon: `'flag'` → **KIRMIZI**.
>
> 🔴 **(3) T1 ÖLÇÜLDÜ: ŞEMADA "reject/ignored ⟹ yön yok" DİYE BİR CHECK YOKTU, VE
> ARTIK İKİ YAZICI VAR.** Canlı sayım (2026-08-10 akşamı, dev DB — bu sayı suit her
> koştuğunda büyür): **331 041** satırın **0**'ı ihlal ediyor. Geri alınan bir sondada
> şema o satırı **kabul etti**. Bu yolu kapatan şey: `internal/domain/tap`'te bir
> **`if`** (birinci yazıcı) ve bir **SQL literali** (ikinci yazıcı).
> ✅ **DÜZELTME TURUNDA KAPANDI: migration 00014 sevk edildi** (orkestratör onayı) —
> `CHECK (verdict IN ('ok','flag') OR type IS NULL)`, **VALIDATED**. Ayrıntı aşağıdaki
> düzeltme bloğunda.
>
> 🔴 **(4) T3 ÖLÇÜLDÜ VE KRİTER 5 EKSİK SPESİFİKASYON: "düzeltmek = yeni satır"
> BORDROYU ANCAK BİR YÖNDE ONARIR.** M6-07'nin eşleme motoru kişinin **en geç
> `in`**'ini ve ondan sonraki **en erken `out`**'unu eşliyor — yani hep **en kısa**
> aralığı. Sonuç: eklenen bir düzeltme satırı bir vardiyayı **kısaltabilir, asla
> uzatamaz**. Ölçüm (bir kişi, gerçek `accumulate`, doğru = 09:00–17:00 = 8 sa;
> `internal/domain/ledger/correction_test.go`):
>
> | hata | eklenen düzeltme | önce | sonra | uygulandı mı |
> |---|---|---|---|---|
> | çıkış çok **ERKEN** (12:00) | `out` 17:00 | 3 sa | **3 sa** | ❌ (fazlalık satır `StartedEarlier`) |
> | çıkış çok **GEÇ** (20:00) | `out` 17:00 | 11 sa | **8 sa** | ✅ |
> | giriş çok **GEÇ** (10:00) | `in` 09:00 | 7 sa | **7 sa** | ❌ (fazlalık satır `Open`) |
> | giriş çok **ERKEN** (08:00) | `in` 09:00 | 9 sa | **8 sa** | ✅ |
>
> **Uygulanmayan ikisi tam olarak parayı GERİ GETİRECEK olan ikisi.** Fazlalık satır
> kaybolmuyor (§4.6) ama *"bu vardiya dönemden önce başladı"* diye etiketleniyor —
> o satır hakkında **yanlış bir cümle**. Çalışan tek telafi **telafi çifti**
> (`in` 12:01 + `out` 17:00 → 7 sa 59 dk, ölçüldü) ve o da dünya hakkında **doğru
> olmayan** bir cümle yazıyor.
> **📌 ORKESTRATÖRE:** tam bir düzeltme akışı ya rapor tarafında *supersede*
> semantiği ya da üçüncü bir tablo ister (ADR 0009 aynı üç seçeneği review'lar için
> sayıyor). **Bu görevde YAPILMADI**; ürün hiçbir yerde *"sonraki kayıt öncekini
> geçersiz kılar"* demiyor ve onay ekranı ölçümü **kelimesi kelimesine** yazıyor.
>
> 🔴 **(5) T2 KARARI: `policy.Evaluate` ÇAĞRILMADI, VE GUARDRAIL'İN CÜMLESİ
> DÜZELTİLDİ.** `sys:occurred-at-bound`'un yorumu *"manual entry is the separate,
> **authorized** `record:manual` action"* diyordu; **"authorized" hiç koşmamış bir
> yetkilendirme adımını** anlatıyordu. İki okuma ölçüldü:
> - **(a) çağırmak:** `policySets.forTenant` `internal/domain/checkin`'de ve
>   **unexported**; dahası **yan etkisi var** — baseline yoksa **materialise ediyor**,
>   yani bir panel POST'u policy tablolarına satır yazardı. Export/taşıma tap yolunun
>   (doğruluk çekirdeği) içine bir dashboard görevinde dokunmak demek.
> - **(b) çağırmamak + cümleyi düzeltmek:** seçilen. Muafiyet **gerçek ve
>   değişmedi** (guardrail `c.Action`'a kapılı), yalnız "authorized" silindi ve
>   yerine manuel kaydın **gerçek** zaman sınırları yazıldı.
> - **Ne satın alırdı, ölçüldü:** `record:manual` **hiçbir guardrail'e çarpmıyor**
>   (10/10, anti-vacuity kontrolüyle) ve baseline onu **owner VE manager**'a veriyor
>   (`base:authz-owner`, `base:authz-manager`); `admin_users`'ın CHECK'i üçüncü bir
>   rol tanımıyor → **bugün kimseyi reddetmezdi**.
>   `TestManualEntryAction_HitsNoGuardrailAndIsGrantedToBothPanelRoles` baseline
>   daralırsa kırmızıya döner. Gerçek kapı **M6-09**.
>
> 🔴 **(6) T6 KARARI: MAC'li onay kapısı GEREKLİ, ve gerekçesi (4)'ün ölçümü.**
> Deaktivasyondan farklı olarak kayıt *telafi edilebilir* görünüyor — ama ölçüm
> gösterdi ki **az ödeme yönünde tek satırlık telafi yolu yok**. Bu, ADR 0009'un
> şekli, parasal biçimde; ADR'nin üçüncü seçeneği (*"hiçbir şey yapma ve ekranda
> söyle"*) **alındı**. ⚠️ **Ve bu kapının subject'i panelde TEK BİLEŞİK olan:**
> `employee/direction/instant` — bir kayıt var olana kadar kimliği yok, yani bağlanan
> şey **beyanın kendisi**. Kişiye bağlı bir onay, "17:00 çıkış" için gösterilen
> uyarının "04:00 giriş" için harcanmasına izin verirdi (mutasyonla kırmızı).
>
> ⚠️ **(7) KRİTERLERDE YAZMAYAN AMA YAZILMASI GEREKEN ALTI ŞEY** (hepsi uygulandı):
> **zaman sınırları** (`MaxBackdate` 90 gün — yıl yanlış yazımını yakalar;
> `FutureGrace` 1 dk; guardrail'in 72 saati **bilinçli olarak** kullanılmadı) ·
> **saat dilimi** (müdür duvar saati yazar, tenant'ın zone'u hangi an olduğuna karar
> verir — §6) · **yön** (`in|out`, kapalı sözlük, **asla default'lanmaz**) ·
> **lokasyon/departman** (istekten **gelmiyor**; INSERT ... SELECT bunları çalışan
> satırından okuyor) · **deaktive çalışan** (reddedilmiyor, **söyleniyor** — son
> vardiya ödenebilir kalmalı, ADR 0010 geri dönüş yolu bırakmıyor) · **ekranın
> nereye indiği** (Transactions, kaydın kendi yerel günü + `channel=manual` — M6-04'ün
> dersi: iddia etme, **satırı göster**).
>
> **ÖLÇÜMLER (2026-08-10, dev DB, gerçek `curl -b jar` oturumu, KF owner).** İki kayıt
> gerçek HTTP ile yazıldı. Yazılan satır: `verdict=ok · channel=manual · type=out ·
> trust=20 · practice=f · queued=f · entered_by dolu · tag_uid/ctr/sun_valid/source_ip/
> ip_match/gps_match/gps_lat/gps_lng/policy_* hepsi NULL`; `occurred_at` 2026-08-09
> 15:00Z (= Malta 17:00), `created_at` yazım anı. Sonra `in` 09:00 girildi ve **CSV
> satırı** şu oldu: `8h 00m · 480 dk · 1 shift · manager_entered_shifts=1 ·
> open_check_ins=0`. Q18'in döngüsü uçtan uca kapandı.
> ⚠️ **SAYILMIŞ LİMİT — iniş sayfası yoğun olabilir.** Yönlendirme doğru günü ve
> kanalı seçiyor ama liste `occurred_at DESC` sıralı: o gün o tenant'ta **659**
> `manual` satır vardı (2026-08-10 akşamı) ve yeni kayıt ilk sayfada değildi. ⚠️ **Bu sayı bir işletmenin
> değil BU DEPONUN özelliği** — 658'in tamamı test süitinin elle eklediği satırlar
> (`seedRecordWithSplitClocks` ve akrabaları); gerçek bir işletmede bir günde birkaç
> manuel kayıt olur. İsim ile daraltmak §4.7 gereği yapılmadı (M6-03 sayfalama
> kursöründen **adı** tam bu sebeple çıkardı).
>
> **MUTASYONLAR — 26 uygulandı, hepsi `diff -u` ile uygulandığı VE geri alındığı
> gösterilerek. Altısı YEŞİL geçti ve altısı da kapatıldı:**
> 1. *verdict literali* → yeşil çünkü **`.sql` düzenlemesi `make sqlc` olmadan
>    ölüdür**; harness düzeltildi (artık `.sql`/`.templ` mutasyonundan sonra üretim
>    koşuyor) → KIRMIZI.
> 2. *tenant yüklemi düşürüldü* → yeşil çünkü **RLS örtüyor**; §4.5'in kuşağı uçtan
>    uca testle **görülemiyor** (§6 zaten bunu söylüyor). Yeni **metin** ağı eklendi
>    (`TestManualQuery_CARRIESTheTenantPredicate…`) → KIRMIZI. ⚠️ Metin iddiası
>    olduğu **kodda yazılı**.
> 3. *audit `direction` değeri boşaltıldı* → yeşil çünkü test **anahtarın varlığına**
>    bakıyordu, değerine değil; JSON parse edilip **değerler** pinlendi → KIRMIZI.
> 4. *`RecordAction` birinci adıma kondu* → yeşil, ve **kodun iddiası yanlıştı**:
>    engel "view'in boş alanı" değil şablondaki `if v.Confirming`. Üç yerde cümle
>    düzeltildi + gerçek engeli süren test eklendi (pozitif kontrollü) → KIRMIZI.
> 5. *roster'ın bağlantısı silindi* → yeşil çünkü mevcut ağ *"her kontrol bir yere
>    gidiyor mu"* diye soruyor, *"kontrol var mı"* diye değil — **M5-04'ün ölü
>    yetenek şekli**; erişilebilirlik testi eklendi → KIRMIZI.
> 6. *not sınırda temizlenmiyor* → yeşil çünkü test **fonksiyonu** sürüyordu, sınırı
>    değil; NUL + aşırı uzunluk gerçek POST'tan sürülüyor → KIRMIZI.
>
> ⚠️ **KENDİ HATAM (iki tane, ikincisi pahalıydı).** (a) Mutasyon harness'ımın ilk
> hâli `.sql` düzenlemesinden sonra `sqlc` koşmuyordu, yani ilk mutasyonu **yanlışlıkla
> yeşil** raporladı — T12'nin *"önce 'uygulanmadı' hipotezini kur"* kuralı işe yaradı.
> (b) **Aynı harness'ın geri-alma yolu `git checkout` kullanıyordu** ve o dosya
> **commit edilmemiş iş taşıyan takipli bir dosyaydı**: `db/queries/transactions.sql`
> HEAD'e döndü ve `InsertManualTransaction` **silindi**. Üretilen
> `internal/store/transactions.sql.go`'dan **tamamı geri kurtarıldı** ve harness
> yeniden yazıldı (artık dosyanın kendi anlık görüntüsünü alıyor, `git`'e hiç
> uzanmıyor). Kayıp yok, ama bu ders yazılıyor: **geri alma HEAD'e değil, ÖNCESİNE
> olmalı.**
>
> ⚠️ **DEV VERİTABANINDA BIRAKILAN İZ (§4.3):** yukarıdaki iki kayıt KF tenant'ının
> `Ahmed Hassan`'ına gerçek panel oturumuyla yazıldı ve **silinmedi** —
> `transactions` append-only. Sessizce temizlemek yerine yazılıyor. DB testleri de
> her koşuda kendi tenant'larını ve kayıtlarını bırakıyor (aynı sebeple).
>
> 🔴 **M6-08'İN DEĞİL AMA M6-08 TARAFINDAN BULUNAN BİR BORÇ — `make test` ARTIK
> DETERMİNİSTİK DEĞİL.** `TestPlaqueJourneyDB_TheBudgetOnATenantWithHistory`
> (M6-06 B) **kendi kendini zehirliyor**: her koşuda demo tenant'a **iki plaket**
> ekliyor (`tags`'ten DELETE yok), sonra yeni eklediğinin **200 satırlık** listede
> (`plaquePageLimit`) görünmesini şart koşuyor. Ölçüldü (2026-08-10): tenant
> **247** plakete ulaştı (245 atanmış + 2 atanmamış), sıralama `location_id NULLS
> FIRST, uid`, yani taze **rastgele uid**'in listeye girme olasılığı ≈ 198/246.
> **Ölçülen:** aynı test tek başına **6 koşuda 5 geçti / 1 kaldı**; tam suit dört
> kez koşuldu, **üçünde 0 FAIL**, birinde yalnız bu test. Oran her koşuda **kötüleşiyor**.
> M6-08'in diff'i plaket koduna, `tags` sorgularına ve locations handler'ına
> **dokunmuyor**. ✅ **DÜZELTME TURUNDA KAPANDI** (E2) — aşağıdaki bloğa bak.


> **Kart düzeltmesi 2 (2026-08-10, M6-08 düzeltme turu — iki denetçi ONAY, sekiz madde).**
>
> 🔴 **ÖNCE: GÜVENLİK MERCEĞİ HEM BENİM HEM ORKESTRATÖRÜN BİR İDDİASINI ÇÜRÜTTÜ, İYİ
> YÖNDE.** *"`verdict='reject' + type='in'` satırı rapora çalışılmış saat olarak
> girerdi"* **yanlıştı**. Gerçek `accumulate` üzerinde ölçüldü ve bağımsız olarak
> doğrulandı:
>
> | satır | worked | awaiting |
> |---|---|---|
> | kontrol: iki uç da `ok` | **8 sa** | 0 |
> | iki uç `reject` + yön | 0 | **8 sa** |
> | iki uç `ignored` + yön | 0 | **8 sa** |
> | `in` ok / `out` reject | 0 | **8 sa** |
> | tanınmayan verdict `'void'` | 0 | **8 sa** |
>
> M6-07 A'nın `endpointState` fail-safe'i o satırı **zaten karantinaya alıyor** —
> asla ödenmiyor, asla kaybolmuyor (§4.6). Yani bariyer **iki değil ÜÇ**, ve
> `decide.go`'nun `if`'i **çıplak bir kod invariantı değil**:
> `TestDecide_DirectionNilForNonRecordVerdicts`'in konusu, mutasyonu **dört alt
> vakada** kırmızı. Düzeltme `manual.go`'nun paket başlığına, `transactions.sql`'e ve
> 00014'ün kendi başlığına yazıldı.
>
> ✅ **E1 — MIGRATION 00014 SEVK EDİLDİ.** `db/migrations/00014_refusal_has_no_direction.sql`,
> `CHECK (verdict IN ('ok','flag') OR type IS NULL)`, **VALIDATED**. Kendi ölçümüm:
> 331 041 satır, **0 ihlal**, `ALTER TABLE ... ADD CONSTRAINT` geri alınan bir
> transaction'da **244 / 200 / 216 / 197 ms**; `convalidated = t`. **Taze bir
> veritabanında da denendi** (throwaway DB, `goose up` → 4,74 ms, sonra `seed`) ve
> `Down` + tekrar `Up` çalıştı. Biçim `NOT IN` değil **`IN ('ok','flag')`**: `verdict`
> NOT NULL (ölçüldü) ve dört değere kısıtlı olduğu için ikisi denk, ama pozitif biçim
> sütun bir gün nullable olursa üç-değerli mantıkla sessizce bozulmuyor. **`Down` bir
> güvenlik geriye-gidişi olarak ADIYLA yazıldı** (00013'ün dersi). ⚠️ **Risk cümlesi
> fazla yazılmadı:** bu bir para sızıntısı düzeltmesi **değil** — *"karantinaya alınan
> bozuk bir satır"*ı *"hiç satır"*a çeviriyor. Mutasyon: `make migrate-down` →
> `TestManualDB_TheSchemaREFUSESADirectedRefusalNow` **KIRMIZI** (`accepted=true want
> false`), `make migrate` → yeşil.
>
> ✅ **E2 — `make test` ARTIK DETERMİNİSTİK.** `TestPlaqueJourneyDB_TheBudget…` iki
> yönden düzeltildi: (1) liste iddiası artık **YEDEK** plakete bakıyor (o `unassigned`,
> yani sorgunun `NULLS FIRST` ilk grubunda — kapaktan bağımsız); bozuk plaket **kendi
> uid'siyle** açılıyor, ki `plaqueByUID` zaten kapağın arkasına düşüyor. (2) Test
> **kendi iki satırını siliyor** (`tappa_owner`, `t.Cleanup`) — paketin *"fixture
> temizlenmez"* kuralı **tappa_app SİLEMEDİĞİ İÇİN** vardır, ve burada temizlememek
> sevk edilmiş bir testi zar atmaya çeviriyordu. `transactions`'a **dokunulmuyor**
> (§4.3). **ÖLÇÜM, aynı makinede, 287 plaketlik tenant'ta:**
>
> | | 8 tek-başına koşum | tenant'ın plaket deltası |
> |---|---|---|
> | düzeltme **olmadan** | **6 geçti / 2 kaldı** | **+16** |
> | düzeltme **ile** | **8 geçti / 0 kaldı** | **0** |
>
> ⚠️ Ölçüm sırasında biriken 16 satır, aynı seansta `tappa_owner` ile **silindi**
> (referansı olmayan, dakikalar önce yazılmış satırlar; `transactions` sayımı 0).
>
> ✅ **E3 — KAPATILAMAZ AÇIK KAYIT: YAZILDI + ADR.** Denetçilerin bulgusu bağımsız
> olarak yeniden üretildi (doğru 09:00–17:00, giriş 10:00 diye yazılmış):
>
> | adım | worked | open | startedEarlier |
> |---|---|---|---|
> | yanlış çift | 7 sa | 0 | 0 |
> | + düzeltici `in`09 | 7 sa | **1** | 0 |
> | + kapatma denemesi | 7 sa | **1** | 1 |
> | + ikinci deneme | 7 sa | **1** | 2 |
> | + üçüncü deneme | 7 sa | **1** | 3 |
>
> ✅ Ve **ürünün asıl senaryosu doğru çalışıyor** (aynı testte, pozitif kontrol):
> `in`09 tek başına → `open=1`; müdür `out`17 yazınca → **8 sa, open=0**. Yani bu bir
> *sayılmış limit*, bozuk bir özellik değil.
> **Nereye yazıldı:** [ADR 0011](../adr/0011-duzeltme-satiri-yalnizca-kisaltir.md)
> (yeni; ADR 0009'un kardeşi, bu kez **parasal**, üç çıkış yolu sayılı) ·
> `manual.CorrectionsOnlyShorten` · `report.go`'nun `accumulate` yorumu · **ve onay
> ekranı**: bir `in` düzeltmesinde artık *"the old one stays in 'needs action' for
> good: nothing you enter afterwards can close it"* diyor. Davranış **değişmedi**.
> Mutasyonlar: cümleyi sil → **KIRMIZI**; cümleyi her yön için göster → **KIRMIZI**;
> `accumulate`'i ilk `in`'i eşleyecek şekilde değiştir → **KIRMIZI**.
>
> ✅ **E4 — REDDEDİLEN YAZMA ARTIK MÜDÜRÜN GİRDİSİNİ KORUYOR.** Üçüncü seçenek
> uygulandı: `renderManualForm` ile **200 ile yeniden render**, not + tarih + saat +
> yön korunarak. Maliyet **ölçüldü ve sıfıra yakın**: handler zaten `f` ve `subject`
> tutuyor, ve `renderManualForm` **review adımının kendi fonksiyonu** — ikinci bir
> render yolu, ikinci bir sözlük yok. Yenilemek güvenli (hiçbir şey yazılmadı, onay
> çerezi bu dalda **temizlenmiyor**). ⚠️ **Tek istisna sayıldı ve gerekçesi yazıldı:**
> yönü okunamayan gönderim hâlâ 303 — geri yazılacak geçerli bir yön yok ve `in`
> tahmin etmek, mislenmiş bir formu kapanmayan bir girişe çevirir. Mutasyon: dalı
> redirect'e geri al → **KIRMIZI** (`answered 303; it must re-render`).
>
> ✅ **E5 — `manager_entered_shifts` YANLIŞ İSİMDİ, İKİ YÖNDE.** Ölçüldü:
>
> | vaka | eski `Manual` | `Shifts` | eski isim |
> |---|---|---|---|
> | tek yazılmış çift | 1 | 1 | şansa doğru |
> | yazılmış girişin düzeltilmesi | **2** | 1 | **ŞİŞİRİYOR** (bordro sütunu) |
> | tap'lenmiş giriş + **yazılmış çıkış** | **0** | 1 | **EKSİK** — ve bu **Q18'in
> kendi vakası** |
> | kapanmamış yazılmış giriş | 1 | 0 | vardiya bile değil |
>
> Aritmetiğe **dokunulmadı** (`arrival()` doğru yer — geç kalma varışın özelliği);
> **isim ölçüme eşitlendi**: `PersonHours.ManualArrivals`, CSV sütunu
> `manager_entered_arrivals`, ekranda *"N arrivals entered by a manager"*. Dört satırın
> dördü de **arrivals hakkında doğru**. ⚠️ Ve *"yazılmış ÇIKIŞ sayılmıyor"* artık
> alanın kendi yorumunda yazılı. Mutasyon: sayacı vardiya-bazlı yap → **KIRMIZI**.
>
> ✅ **E6 —** radyo düğmeleri `accent-tappa-green` aldı (aktivasyon ekranının kendi
> onay kutularıyla aynı sınıf; tarayıcı mavisi paletin dışında). CSS **büyümedi**
> (sınıf zaten derleniyordu). Mutasyon: sınıfı sil → önce **YEŞİL** (ağ yoktu), test
> eklendi (anti-vacuity: radyo sayısı = kapalı sözlüğün boyu) → **KIRMIZI**. Bayat
> as-of sayıları tazelendi.
>
> ✅ **E7 — ÖLÇÜLDÜ VE UYGULANDI (zorunlu değildi).** Denetçinin mutasyonu
> (`OR e.tenant_id IS NOT NULL`) benim metin ağımı ve çapraz-tenant DB testimi
> **ikisini de** yeşil bırakıyordu. `internal/domain/tenant`'ın **türetilmiş**
> eşleştiricisi ise onu **yakalıyor** — ölçüldü, beş gevşetme mutasyonunun **beşi de**:
> `OR ... IS NOT NULL` · `OR TRUE` · totoloji · yüklem silinmiş · parametre yerine
> sabit. **Genişletme maliyeti:** tüm `db/queries` üzerinde **70 sorgunun 2'si** yanlış
> alarm (%2,9), **ikisi de aynı şekil** (dış takma ada bağlı korelasyonlu alt sorgu).
> `transactions.sql`'e genişletmek **1** yanlış alarm demek (`GetLastOpenTransaction`).
> **Seçilen:** dosya bazlı belt `transactions.sql`'e genişletildi, o **tek istisna
> gerekçesiyle** bir allowlist'e yazıldı (ve *"gereksizleşen istisna silinir"* diye
> pinlendi). 🔴 **BEKLENMEDİK BULGU:** eşleştirici `INSERT ... VALUES`'ü **hiç
> göremiyor** — tenant tablosunu yalnız FROM/UPDATE/JOIN üzerinden buluyor. Yani
> `InsertTransaction` bu belt'in **kör noktasında**, ve `InsertManualTransaction`
> ancak `INSERT ... SELECT` olduğu için kapsanıyor. Kör nokta **sayıldı, adlandırıldı
> ve kapatılmadı** (ayrı bir mutasyon turu ister). Mutasyon: yüklemi gevşet →
> **KIRMIZI**.
>
> ✅ **E8 — ÖLÇÜLDÜ VE KISMEN UYGULANDI (zorunlu değildi).** ⚠️ **Sayı artık burada
> değil, TÜRETİLİYOR:** `TestPanelProblemPages_CountTheWriteRoutesStillTellingReaders
> TheirPageIsEmpty` paketi `go/ast` ile geziyor, çağrı yerlerini `mountWriting`'in
> kayıtlı rotalarına göre sınıflandırıyor ve her koşuda basıyor. (Elle yazdığım ilk
> sayı **yanlıştı** — grep kendi yorumumdaki geçişi de saymıştı.) İkinci bir `ProblemView` eklendi
> (`problemPanelWriteFailed` — *"nothing was written: no record, no change and no trail
> entry"*), ve **yalnız bu görevin üç çağrı yeri** çevrildi. İkinci temsil riski
> **düşük**: aynı olgunun ikinci kopyası değil, **farklı bir olgunun** cümlesi
> (`problemAdminTooMany` emsali). Kalan 13 + 5 **sayıldı ve fiyatlandırıldı**, sahibi
> M6-04/05/06. Mutasyon: yazma dalını okuyucunun cümlesine geri al → **KIRMIZI**
> (pozitif kontrolüyle: okuma yolu kendi cümlesini koruyor).
>
> ⚠️ **KENDİ HATALARIM (bu turda iki tane).** (a) E6'nın mutasyonu ilk koşuda
> **YEŞİL** geçti çünkü marka aksanı için hiç test yazmamıştım — mutasyon önce
> **ağın yokluğunu** buldu. (b) E4 davranışı değiştirdiği için M6-08'in kendi
> `TestManualEntryRecord_RefusesWithoutTheConfirmation…` testi 303 bekliyordu ve
> kırmızıya döndü; iddia **yeni davranışa** taşındı (200 + yeniden render), iki gerçek
> savı korunarak.

> **Kart düzeltmesi 3 (2026-08-10, M6-08 ikinci düzeltme turu — KIRMIZI verdict:
> sekiz düzeltmenin sekizi de davranışsal olarak doğruydu, ama tur kendi
> değişikliklerinin tersini söyleyen dört metin bıraktı).**
>
> 🔴 **BU, BU PROJENİN İMZA SINIFININ DÜZELTME TURUNDA GÖRÜNMESİ:** *bir düzeltme,
> aynı dosyadaki (ya da komşu dosyadaki) başka bir cümleyi geçersiz kılar.* Dördü de
> kapandı; üçü **§4.3/§4.6 yüzeyindeydi**.
>
> 🔴 **B1 · `checkin.go`** — 00014'ten sonra üç şeyi birden yanlış söylüyordu:
> kuralın *"şema kısıtı değil kod invariantı"* olduğunu, veritabanının o satırı
> *"kabul ettiğini"*, ve **repoda olmayan bir test adını**. Bariyer artık **dört**
> ve dördü de adlarıyla yazılı (`decide.go`'nun `if`'i **+ kendi testi** ·
> manuel yolun SQL literali · `endpointState`'in `HoursAwaiting` karantinası ·
> **00014**), atıf **var olan** teste (`TestManualDB_TheSchemaREFUSESADirectedRefusalNow`).
> Eski cümle silinmedi, **düzeltmesiyle birlikte** duruyor.
>
> 🔴 **B2 · `manualentry_db_test.go`** — aynı iki iddiayı tekrarlıyordu, üstelik
> ikincisini (*"rapor bunu çalışılmış saat okur"*) 00014'ün **kendi başlığı FALSE
> ilan etmişti**. Yani aynı repoda iki dosya birbirini yalanlıyordu. Kapandı.
>
> 🔴 **B3 · `manualentry.go`** — *"EVERY OUTCOME ANSWERS 303"* diyordu, oysa aynı
> turun E4 düzeltmesi **beş dalı 200'e** çevirmişti. Cümlenin gerçekte savunduğu
> özellik (*"yenileme ikinci bir kalıcı satır yazamaz"*) **yeniden ölçüldü** ve durum
> kodundan bağımsız çıktı: onay reddi `Record`'dan **önce** ve çerez **temizlenmeden**
> dönüyor (F5 aynı kapıya çarpıyor); domain reddi çerez **temizlendikten sonra**
> oluyor (F5 → `confirm-required`). İkisi de uçtan uca pinli:
> `TestManualEntryRecord_ARefreshOfARefusedWriteCannotAppendASecondRecord`
> (pozitif kontrolüyle: onaylı yazma **tam olarak bir** satır üretiyor).
>
> 🔴 **B4 · 00014'ün gerekçesi ölçümle yanlıştı — kendi sondamla doğruladım.**
> *"`NOT IN` sütun nullable olursa sessizce bozulur"* **yanlış**: iki biçim de
> **aynı** şekilde bozuluyor (`NULL` sonuçlu CHECK sağlanmış sayılıyor —
> `INSERT (NULL,'in')` ikisinde de **KABUL**). Pozitif biçimin gerçek faydası başkaydı
> ve yazılı değildi, o da ölçüldü: **sözlüğe yeni bir verdict eklenirse**
> `NOT IN ('reject','ignored')` yönlü bir `'void'`'i **KABUL** ediyor (fail-open),
> `IN ('ok','flag')` **REDDEDİYOR** (fail-closed). Migration başlığı bu repoda
> normatif metin; cümle ölçüme eşitlendi ve **iki formun da denendiği** yazıldı.
>
> ✅ **B5** — `mountWriting`'in yorumundan **çıplak sayı silindi** (üçüncü kez
> kaymıştı: "sekiz ve dört" → "on bir" → 13). Kural uygulandı: *kodun sahip olduğu
> bir kümeyi tarif eden çıplak tamsayı yorumlarda yer almaz.*
> ✅ **B6** — `reportscsv.go` artık var olmayan `PersonHours.Manual`'a *"a BOOLEAN"*
> diyordu. İki sütun **ayrı ayrı** tarif edildi: `manager_entered_arrivals` bir
> **SAYI** (`ManualArrivals`), `manager_entered` bir **BOOLEAN** (`OpenEntry.Manual`).
> ✅ **B7** — sayı üç yerden **silindi** ve **türetildi** (yukarı bak). Census bugün:
> **32** kullanım, **14'ü** `mountWriting` rotalarında, **3** yeni yazma cümlesi,
> M6-08'inkiler listede **yok** (tek assertion bu).
> ✅ **B8** — kör nokta **ürün geneli** sayıldı: 70 sorgunun **12'si INSERT**, bunların
> **7'si `INSERT ... VALUES`** ve eşleştiriciye görünmez (`RecordAuditEvent` ·
> `CreateInvite` · `EnsureBaselinePolicy` · `EnsureBaselinePolicyVersion` ·
> `EnsurePolicyAttachment` · `CreateSession` · `InsertTransaction`); 5'i
> `INSERT ... SELECT` ve tutulabiliyor. **Ve iki kaçış yolu daha sayıldı:** (i)
> eşleştirici INSERT'ün **yazdığı** `tenant_id` değerine hiç bakmıyor, (ii) yalnız
> işaret edildiği dosyaları kapsıyor. **Üçü de RLS ile kapalı** — belt deliği, canlı
> açık değil. Yazıldı, kapatılmadı.
> ✅ **B9** — `venue.templ`'in **iki** checkbox'ı (`venue-overnight`, `dept-overnight`,
> M6-06) aksansızdı → tarayıcı mavisi. Düzeltildi **ve ürün geneli test yazıldı**
> (`TestBrand_EveryNativeCheckboxAndRadioCarriesTheAccent`, 5 kontrol tarıyor,
> anti-vacuity tabanlı) — çünkü F6'da mutasyon **ağ olmadığı için** yeşil geçmişti.
> Mutasyon: her iki checkbox ayrı ayrı → **KIRMIZI**. CSS büyümedi.
>
> 🔴 **B10 — CSV SÜTUN ADI DEĞİŞİMİ BİR SÖZLEŞME KIRILIMIDIR.**
> `manager_entered_shifts` → **`manager_entered_arrivals`** (E5). Ürün pilotta değil ve
> bu dosyayı bugüne kadar yalnız bu deponun testleri indirdi — ama **bir CSV sütun adı
> muhasebecinin makrosuna girer**, ve bir kez bir müşteriye gittikten sonra aynı
> değişiklik sessizce bir bordro tablosunu boş bırakır. Şimdi yapılmasının sebebi tam
> olarak bu: eski ad **iki yönde birden yanlıştı** ve yanlış bir adı taşımaya devam
> etmek daha pahalıydı. ⚠️ **Bir sonraki sütun adı değişikliği pilot sonrası bir sürüm
> notu ister** — bu satır o kuralın kaydıdır.
>
> ⚠️ **KENDİ HATALARIM (bu turda üç).** (a) B7'nin sayısını **elle** yazmıştım ve
> yanlıştı — grep kendi yorumumdaki geçişi de sayıyordu; artık türetiliyor.
> (b) B8'i **yanlış kapsamda** yazmıştım (*"bu dosyada tam olarak BİR ifade"* doğru,
> *"ürün geneli"* değil). (c) B3'ün yeni testinin ilk hâli iki vakayı `c.name[0]`
> ile ayırıyordu ve **ikisi de 'a' ile başlıyordu**, yani aynı şekli iki kez sürüyordu;
> açık bir alan (`dropConfirmation`) eklendi.

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
- ~~Politika kaydetmeyi "hemen yürürlüğe girer" yapma; M6-10 simülasyonu önce
  gösterilmeli.~~ — **bu tuzak kendi kartıyla çelişiyordu, aşağıdaki 2026-08-11
  bloğuna bakın.**

> ### Kart düzeltmesi (2026-08-11, M6-09 faz A uygulaması)
>
> **1. Görev iki faza bölündü** (orkestratör kararı). Ölçüt kapsam değil
> **denetim merceği**: faz A'nın merceği *"görünür ama kapatılamaz" · §4.5 ·
> §4.7 · marka · yan etkisiz okuma*, faz B'ninki *yazma, yeni sürüm, audit_log,
> aralık doğrulama, `policy:edit` zorlanması*. Aynı bölünme M6-06 (üç parça) ve
> M6-07 (iki parça) için de doğru çıkmıştı.
>
> - **Faz A (bu tur, tamamlandı):** `/admin/policies` okuma yolu — üç katman,
>   guardrail sırası, sürüm geçmişi, yetkilendirme bölümü. **Hiçbir şey yazmaz.**
> - **Faz B (ayrı tur):** baseline aç/kapa · tenant politikası oluştur/düzenle
>   (**yeni sürüm**, üzerine yazma yok) · `resource` bağlama · `audit_log` ·
>   aralık doğrulama · `policy:edit` guardrail'inin **gerçekten** zorlanması.
>
> **2. 🔴 "M6-10 simülasyonu önce gösterilmeli" tuzağı yürürlükte DEĞİL — kart
> kendi içinde çelişiyordu.** [M6-10](#m6-10--policy-simülatörü---⛔-ertelendi--m9-06)
> `skipped`: Q22 simülatörü M9-06'ya erteledi ve bunu bu kartın hemen altındaki
> blok yazıyor. Yani "kaydetmeden önce simülasyonu göster" v1'de **karşılanamaz**
> bir koşuldur. Faz B'nin "hemen yürürlüğe girer" endişesine vereceği cevap
> simülasyon **olamaz**; elde kalan üç seçenek ve maliyetleri:
> (a) kaydetmeden önce **değişikliğin metnini** gösteren bir onay adımı — ucuz,
> ama "ne değişir"i söylemez; (b) yazma sonrası **geri alma** — `policy_versions`
> append-only olduğu için "geri al" da yeni bir sürümdür, yani ucuz **değildir**
> ama mümkündür; (c) **hiçbiri** — ve bu, sayılmış bir limit olarak yazılır.
> **Karar faz B'nindir; burada yalnız çelişki kaydedilmiştir.**
>
> **3. Faz A'nın karşıladığı kriterler.** Üç katman ayrı ayrı · guardrail'ler
> kilit + gerekçe (**kapatma kontrolü ekranda yok — model tipinde de alan yok**)
> · guardrail **sırası** `policy.Guardrails()`'ten **türetiliyor** (elle yazılmış
> liste yok; ADR 0007 bandı dahil) · sınırlı parametreler **aralıklarıyla ve
> yürürlükteki değeriyle** (`checkin.Service.Params()`'ten — ikinci bir kurulum
> yok) · sürüm geçmişi *kim/ne zaman* · yetkilendirme bölümü ayrı ve
> **fail-closed varsayılan ekranda yazılı**, hem de `policy.Evaluate`'e sorularak
> **türetilmiş**.
>
> **4. 🔴 BOZUK TEK BELGE — denetim bulgusu, faz A'da KAPATILDI (2026-08-12).**
> Stored bir baseline belgesi parse edilmezse `checkin.forTenant` onu `missing`
> sayar, materialise **no-op** kalır (`ON CONFLICT DO NOTHING`), yeniden okur ve
> **yalnız guardrail'lerle** karar verir — baseline'ın *tamamı* ve tenant katmanı
> düşer. İlk sürüm o **bir** kuralı doğru gösterip diğer sekizini `In force` diye
> basıyordu, ve *"Who may do what"* bölümü motorun uygulamadığı izinleri
> *"Granted to"* diye listeliyordu. Kapatma: `PolicyScreen.GuardrailsOnly` +
> `Unreadable`; bu durumda sayfanın **başında** `ToneAlert` bir uyarı, hiçbir
> kurala **`In force` etiketi verilmiyor** (`Stored — but not deciding today`),
> ve yetkilendirme bölümü **aynı bayrağı** taşıyor. Gerçek Postgres'te ölçüldü
> (`{}` belgeli bir tenant kuruldu): sayfada `In force` **0 kez** geçiyor.
> Motor tarafındaki yarı zaten pinli:
> `checkin.TestPolicySetDB_ACorruptStoredDocumentFallsBackToGuardrailsAlone`.
>
> **4b. 🔴 "HİÇBİR KONTROL YOK" AĞI ALTI KEZ YENİLDİ — İKİSİ TÜRETİMLE KAPANDI,
> İKİSİ SAYILDI (2026-08-12).** Her turda **ürün temizdi** (sevk edilen şablonda
> kontrol yok, view'da alan yok); kırılan **korumaydı**. Kök neden hep aynıydı:
> ağın *taradığı* bölge, taradığını *söylediği* bölge değildi.
>
> **KAPANDI — türetim 1: bölüm DIŞI artık tarif edilmiyor, TÜRETİLİYOR.** Kabuk
> ayrı bir şablon: `pages.PanelShell` **boş gövdeyle** render ediliyor ve sayfa
> *bayt bayt* "kabuğun başı + bu bölümün sarmalayıcısı + kabuğun sonu" olmak
> zorunda. Bundan önceki hâl kabuğun **ölçülmüş 38 token'ı**nı muaf tutuyordu ve
> denetçi kontrolü **kabuğun kendi çıkış formunun sözcük dağarcığıyla** kurdu
> (`<form method="post" action=…><button class="btn min-h-11" name type="submit">`)
> — hiçbir token kümesi ikisini ayıramaz, çünkü fark sözcükte değil **varlıkta**.
> Token listesi tamamen kaldırıldı. Mutasyon: aynı kontrol → **RED** (sekiz
> fixture'ın hepsinde); yanlış-alarm yönü: bölüm İÇİNE sıradan bir paragraf →
> **yeşil**.
>
> **KAPANDI — türetim 2: fixture kümesi `.templ`'den.** `policyTemplBranches` her
> koşullu dalı çıkarıyor, `policyBranchWitness` her dala bir tanık veriyor, test
> "en az bir fixture render ediyor / en az biri etmiyor" diyor; harita **iki
> yönlü**. İlk koşuda **sekiz dalı hiçbir fixture'ın sürmediği** ölçüldü —
> `a.Guardrail != ""` dahil, ki o blok her üretim sayfasında iki kez render
> ediliyor. Bugün **27 dal / 8 fixture**.
>
> **🔴 SAYILDI (kapatılmadı, kapatıldığı da iddia EDİLMİYOR):**
> 1. **`switch`/`case` ve düz Go fonksiyonu + `templ.Raw` görünmez.** Dal
>    türetimi **satır** okuyor. ⚠️ Bu madde bir tur boyunca *"repoda **üç**
>    şablonda"* diyordu; gerçek **üç katı**. Sayı artık **elle yazılmıyor**:
>    `TestPolicyBranchDerivation_ReportsTheSizeOfItsOwnBlindSpot` aynı `inTempl`
>    mantığıyla `web/templates` altını tarayıp günün rakamını **basıyor**, boş
>    çıkarsa limit maddesinin silinmesini istiyor, ve `policies.templ`'in kendisi
>    bir `switch` kazanırsa **kırmızı** oluyor. Bulunan `switch`lerden biri
>    (`plaque.templ`) bir `<form>` basıyor — bunlar süs değil, markup üreten
>    dallar.
> 2. **Ödünç tanık geçer.** Ayırt etme kontrolü tanığın **fixture'lar arasında**
>    ayırdığını ölçüyor, **dalın onu ürettiğini** değil: başka bir bloğun bastığı
>    bir dizeyi tanık yazan yeni bir blok testi geçer.
>
> İkisi de gerçek bir templ ayrıştırıcısı ister. Bu ağ **üç kez yamandı ve her
> seferinde yenildi**; yarım çalışan bir dördüncü yama, okunabilir bir limitten
> daha kötüdür.
>
> **🔴 ALTINCI KAÇIŞ — TARAYICININ KENDİSİ (2026-08-12, kapatıldı).** Etiket
> regex'i `[^>]*` ile okuyordu, yani bir **öznitelik değerinin içindeki ham `>`**
> etiketi ortadan kesiyordu: `<p class="docket-label>" contenteditable="true"
> role="switch" …>` sarmalayıcının **içinde** ve `interactiveMarkup` **boş**
> dönüyordu — üstelik kullanılan iki öznitelik testin kendi kalıcı negatif
> kontrolünde tuttuğu iki kaçış. Çözüm **tam ayrıştırıcı değil** (stdlib'de HTML
> tokenizer yok, §1 yeni bağımlılığa kapalı): tarayıcı **okuyamadığını
> REDDEDİYOR** — öznitelik parçasında tek sayıda `"` görürse bulgu üretiyor.
> Yanlış-alarm yönü ölçüldü: templ `>` ve `"` karakterlerini öznitelik değerinde
> kaçırdığı için (`&gt;`, `&#34;`) dokuz fixture'ın hiçbirinde ateşlemiyor.
>
> **EK SAYILMIŞ LİMİTLER (bu tur eklendi):** sarmalayıcının üstüne konan ve
> kabuğun kapanış baytlarının (`</main></body></html>`) **öneki** olan bir dize
> karşılaştırmayı kaydırıp geçer — yalnız kapanış etiketiyle kurulabildiği için
> **kontrol taşımaz**, belge yapısını bozar · **kabuğa** (`panelChrome`) konan bir
> kontrol bu ağın öznesi değil; onu diğer bölümlerin rota ve dokunma-hedefi ağları
> koruyor · dal anahtarı `<koşul>@<sıra>` **satır taşımada** stabil, aynı koşuldan
> birinin **öne eklenmesinde** değil (gürültülü kırılır, sessiz değil).
>
> **AĞIN TUTTUKLARI** (her biri kırmızıya giden bir mutasyonla ölçüldü):
> sarmalayıcının **içinde** her yerde (koşullu bloklar dahil) inert olmayan her
> eleman/öznitelik · sarmalayıcının **dışında** her şey (kabuğun kendi sözcük
> dağarcığıyla kurulmuş kontrol dahil) · fixture'sız yeni `if` · `else` dalı ·
> yalnız üretilen `_templ.go`'ya yazmak · üç öz-fren (ölü allow-list girişi, hiç
> render edilmeyen tanık, her fixture'ın render ettiği tanık).
>
> **5. Faz A'nın SAYILMIŞ limitleri** (kapatılmadı, kapatıldığı da iddia
> edilmiyor):
> - **Eski sürümün GÖVDESİ gösterilmiyor.** Geçmiş şeridi sürüm no · tarih ·
>   yazar · **bayt** verir; belgenin kendisini vermez. Gerekçe: `DefaultLimits`
>   belge/sürüm/bayt sınırlarını **ayrı ayrı** koyuyor, yani satır sınırı bayt
>   sınırı değildir ve bir sayfa dolusu azami belge megabaytlarca istek eder.
>   Sınırlı ayrı bir rota gerekiyor → **faz B**. Ekran bunu **söylüyor**.
> - **`ListPolicySet`'in LIMIT'i yok** ve bu **düzeltilmedi**: o sorgu tap
>   yolunun sorgusu, LIMIT eklemek karar setini sessizce budar ve kısmi baseline
>   *daha zayıf* değil *farklı* bir politikadır. Panel, tap yolunun zaten
>   taşıdığı bir açıklığı miras alıyor → M7-03/faz B.
> - **`policy_attachments` karar vermiyor.** Motor ifadenin **içindeki**
>   `resource` desenine bakıyor; ekran ikisini de basıyor ve **hangisinin
>   bağladığını** yazıyor.
> - **Panel motoru KAPI olarak çağırmıyor.** Ölçüm düzeltildi: `internal/handler`
>   0, ama `internal/domain/tenant/rulebook.go` **her render'da** `policy.Evaluate`
>   çağırıyor (boş `Set` ile, yalnız fail-closed varsayılanı **basmak** için).
>   Test artık **iki paketi birden** tarıyor ve gerekçeli bir **allowlist**
>   tutuyor, yani faz B yetkilendirmeyi o pakete koyarsa test kırmızı olur.
> - **Sürüm okuması TENANT genelinde sınırlı.** Kesilme, versiyonsuz bir
>   container'ı düşürüp durumu *"hiç kurulmamış"* gibi gösterebiliyordu; artık
>   **`VersionsCapped`** iken o durum **`Cannot be determined on this page`**
>   olarak raporlanıyor (iki durum farklı eylem gerektiriyor). ⚠️ Bu madde bir tur
>   boyunca *"`Capped` iken"* diyordu ve **aynı bloğun bir sonraki maddesiyle
>   çelişiyordu**; kod `if out.VersionsCapped` idi, yani yanlış olan madde metniydi.
> - **GPS yarıçapı** (25–1000 m) sınırlı parametredir ama `policy.Params`'ta
>   yoktur → faz A'da gösterilmiyor.
> - **Okuma yetkisi role bakmıyor:** owner da manager da okuyabilir. *Düzenleme*
>   owner-only kalır (`sys:policy-edit-owner-only`), faz B onu zorlar.
> - **Kilit iddiası tek yerde.** Eylem kartındaki cümle artık *"the engine would
>   refuse it"* diyor; *"nobody may do it"* **kaldırıldı** çünkü panel motora
>   sormuyor ve bir müdür rapor dışa aktarabiliyor. Kilit iddiası **yalnız** bölüm
>   sonundaki, iki yönlü testli bildirimde.
> - **İki tavan ayrıldı — domainde VE ekranda.** `AttachmentsCapped` bir kuralın
>   durumunu bilinmez yapmıyor; yalnız `VersionsCapped` yapıyor (ölçüldü: 401
>   attachment + eksiksiz versions → eskiden 9/9 *"bilinmiyor"*, şimdi 9/9 doğru
>   durum). ⚠️ İlk düzeltme **yalnız domaindeydi**: ekran ikisi için tek uyarı
>   basıyordu ve attachment tavanı tek başına çarptığında **üç yanlış şey**
>   söylüyordu (*"geçmişiniz uzun"* · *"bazı sürümler listelenmedi"* · *"dışarıda
>   kalan kural bunu söyler"*), kesilen liste ise **"Bound to"** idi ve tam
>   sanılacak şekilde basılıyordu. Artık **iki ayrı bildirim**, her biri kendi
>   kesilen listesini adlandırıyor.
>
> **6. Ölçümler — as-of 2026-08-12.** ⚠️ **Bu blok iki kez yeniden ölçüldü ve ilk
> hâlindeki üç sayı yanlıştı** (KF 34.560 / bozuk 22.998 — sayfa o ölçümden sonra iki kez değişti ve
> blok güncellenmedi; ayrıca `/admin` için tenant yazılmamıştı). Aşağıdaki tablo
> **2026-08-12**, geliştirme veritabanı, gerçek imzalı oturumla `curl`, port 8099
> `lsof` ile doğrulandı — **her satır tenant'ıyla ve koşuluyla**:
>
> | sayfa | tenant / koşul | bayt |
> |---|---|---|
> | `/admin/policies` | KF — 9 baseline materialise edilmiş, hepsi okunur | **34.593** |
> | `/admin/policies` | KM — hiç materialise edilmemiş | **18.833** |
> | `/admin/policies` | demo tenant — bir belge okunamıyor (`{}`) | **23.221** |
> | `/admin` | KF | 32.276 |
> | `/admin` | KM | 4.501 |
>
> KF'de okuma öncesi/sonrası `policies/policy_versions/policy_attachments` sayımı
> **9/9/9 → 9/9/9**, KM'de **0/0/0 → 0/0/0**, bozuk tenant'ta **2/2/2 → 2/2/2**.
>
> ⚠️ `/admin` KF'de her `make test` koşusuyla büyüyor (paket o tenant'a kayıt
> yazıyor), bu yüzden o satır bir **gözlem**, bir hedef değil. Tablodaki
> `/admin/policies` satırları ise sayfanın **yapısına** bağlı: KF'de dokuz kural
> materialise edilmiş, KM'de sıfır, demo tenant'ta iki (biri okunamaz). Bir sayı
> tekrar değişirse **sayfa değişmiş demektir** — o zaman yeniden ölçülmeli, tahmin
> edilmemeli.

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
