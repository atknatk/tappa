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
