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
- **Deaktive → oturum o saniye ölür** (`revoked_at`), sonraki tap `reject`.
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
