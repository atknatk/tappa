# ADR 0021 — `op_*`: tenant sınırını aşmanın TEK yolu

- **Durum:** kabul edildi — kullanıcı kararı **D-B** (2026-09-24,
  [m10-platform.md](../plan/m10-platform.md) §6). **Uygulama: yok** — bu ADR
  yazıldığında (HEAD `f6f5a9b`) `tappa_operator`, `tappa_opdefiner`, `platform_*`
  tabloları ve tek bir `op_*` fonksiyonu **yoktur** (katalogda ölçüldü).
  **OP-5 (2026-09-26):** roller, dört tablo ve ilk beş `op_*` migration `00026` ile doğdu —
  bkz. "OP-5 uygulama notu". **OP-10 A fazı (2026-10-03):** `op_begin_read`, ilk `op_read_*`
  (`op_read_legal_versions`) ve `op_publish_legal` migration `00027` ile doğdu; `tappa_app`
  `legal_documents` üzerindeki INSERT'ini kaybetti — bkz. "OP-10 uygulama notu".
  **OP-11 A fazı (2026-10-03):** tenant verisini okuyan ilk iki `op_read_*`
  (`op_read_tenants`, `op_read_tenant_detail`) migration `00029` ile doğdu; `op_begin_read`
  iki okuma türü kazandı — bkz. "OP-11 uygulama notu". **OP-11 B fazı (2026-10-03):** tenant
  listesi, araması ve genel bakışı operatör yüzeyinde (`/operator/tenants`,
  `/operator/tenants/{id}`), `*OperatorDB`'nin iki yeni yöntemiyle; migration yok — bkz. aynı
  notun "OP-11 B fazı eki". **OP-13 A fazı (2026-10-03):** bir tenant'ın plaket envanterini
  okuyan `op_read_tenant_plaques` migration `00030` ile doğdu; `op_begin_read` bir okuma türü
  (`tenant_plaques`) kazandı; tanımlayıcı `tags`'in iki anahtar dışındaki bütün sütunlarını
  okur, iki anahtarda (`aes_key_ref`, `app_key_ref`) sütun yetkisi yoktur; ölçülen okuma
  biçimleri "OP-13 uygulama notu" PART I'de. Ekran B fazıdır. **OP-13 B fazı (2026-10-06):**
  plaket envanteri operatör yüzeyinde (`/operator/tenants/{id}/plaques`), `*OperatorDB`'nin
  yeni `TenantPlaques` yöntemiyle; okumalar oturum başına ayrı bir okuma bütçesi
  (`readLimit` 60 / 10 dk) öder, `sessionLimit` istek sayar (100); migration yok — bkz. aynı
  notun "OP-13 B fazı eki".
  **OP-14 A fazı (2026-10-06):**
  operatörün kendi audit log'unu okuyan `op_read_audit` migration `00031` ile doğdu;
  `op_begin_read` bir okuma türü (`operator_audit`) kazandı; `op_record_auth_event`'in kapalı
  kümesine `password_ok` girdi (§1 tablosu ve sınır 7 aynı değişiklikte düzeltildi) — bkz.
  "OP-14 uygulama notu". Ekran (B) ve `password_ok` yazıcısı (C) ayrı fazlardır.
  **OP-12 A fazı (2026-10-07):** bir tenant'ın faturalama aylarını okuyan
  `op_read_tenant_billing` migration `00032` ile doğdu — fatura aritmetiğinin **üçüncü
  kopyası**, tenant'ın kendi yoluna ay ay bir testle bağlı; `op_begin_read` bir okuma türü
  (`tenant_billing`) kazandı, `op_read_audit` o kapsamı tanır; tanımlayıcı 00016'nın beş fatura
  fonksiyonunda EXECUTE aldı — `op_*` dışı EXECUTE kümesi artık adlı bir listedir — bkz.
  "OP-12 uygulama notu". Ekran B fazıdır.
  **OP-14 B fazı (2026-10-07):** audit günlüğü operatör yüzeyinde (`GET`/`POST
  /operator/audit`, konsoldan link), `*OperatorDB`'nin yeni `OperatorAudit` yöntemiyle; her
  görüntüleme bir okuma birimi; migration yok — bkz. aynı notun "OP-14 B fazı eki".
  **OP-14 D (2026-10-07, K14-2):** platform sahibinin üç `cmd/opadmin` eylemi
  (`create`, `reset-mfa`, `disable`) migration `00033` ile birer `operator_audit_log` satırı
  yazar — bir definer'dan değil, sahibin SQL'inin `DO` bloğundan (ADR 0020 §5'in adlı
  istisnası); `actor_shape` üçüncü kolu aldı; bir `BEFORE INSERT` tetikleyicisi her satırın
  `at`'ini duvar saatine zorlar (OP-14 notu md. 7 ve sınır L2 kapandı); `op_begin_read` ve
  `op_read_audit` yalnız birer kümeleriyle genişledi — bkz. "OP-14 D uygulama notu".
- **Tarih:** 2026-09-26 · aynı gün **2. tur** (güvenlik denetiminin RED'i: izsiz okuma),
  **3. tur** (güvenlik denetiminin RED'i: bilet süresinin saati; üçüncü gözün bulguları) ve
  **3. tur eki** (orkestratör kararı: enrollment ve kimlik bilgisi yazımı definer'da) ve
  **4. tur** (iki merceğin ORTA/DÜŞÜK bulguları: donan saatler, bilet INSERT sütunları,
  TOTP adımı zehirlemesi, yazılan zaman damgaları, arama terimi) işlendi; plan tarafındaki sapma listesi
  [m10-platform.md](../plan/m10-platform.md) → OP-4 kart düzeltmesi.
- **Bağlam:** M10 Akış A, görev OP-4 ·
  [ADR 0002](0002-tenant-baglami-ve-rls.md) md.6'nın öngördüğü *"ayrı bir rol ve ayrı
  bir ADR"*
- **İlgili:** [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) (operatör kimliği —
  bu ADR'nin uygulama yarısı) · ADR 0002 md.1, md.6, md.7 ·
  [ADR 0016](0016-tenant-kapsamsiz-operator-icerigi.md) ·
  [ADR 0017](0017-encode-rolesi-ve-yarim-yazma-kurtarmasi.md) §3.1 (havuzun ayrıcalıklı
  rol reddi) · [ADR 0015](0015-sifirlama-tokeni-tek-gecislik-yetkidir.md) (tek ifadelik
  tüketim ve şema süre tavanı emsali) · CLAUDE.md §4.5, §6, §7

## Tek cümlede

🔴 **Bu, CLAUDE.md §4.5'in BİLEREK ve TEK YERDE aşılmasıdır.** Bir tenant'ın satırını o
tenant'ın bağlamı olmadan okuyan ya da yazan her şey, adı `op_` ile başlayan,
`tappa_opdefiner`'a ait bir `SECURITY DEFINER` fonksiyondur ve onu yalnız
`tappa_operator` çağırabilir. Başka bir yol yoktur. Bu ADR o yolun sınırını normatif
koyar: hangi sütunların **asla** okunmadığı, hangi eylemin **nereye** audit yazdığı,
🔴 **veri döndüren her `op_*`'ın veriyi ancak o okumanın audit satırı COMMIT edildikten
sonra vermesi** (§2 v) ve 🔴 **her süre karşılaştırmasının duvar saatiyle
(`clock_timestamp()`) yapılması ve yazılan her zaman damgasının duvar saatinden gelmesi**
(§2 vii). §4.5'i aşma yetkisi kullanıcının D-B
kararından gelir; bu ADR o kararın **nasıl** sınırlandığıdır.

## Neden ayrı bir ADR

**ADR 0002 md.6:** *"Süper-admin / çapraz-tenant erişim MVP'de yoktur. Böyle bir ihtiyaç
doğarsa ayrı bir rol ve ayrı bir ADR ile gelir. `tappa_app` bu amaçla asla
ayrıcalıklandırılmaz; ona `BYPASSRLS` verilmez."* Bu, o ADR'dir. md.6'nın iki koşulu
korunur: roller **ayrı**, `tappa_app` **hiçbir yeni yetki almaz**. ADR 0002'nin metni
değişmez.

**md.7 ile akraba ama aynı sınıf değil.** md.7'nin çözümleyicileri tenant bağlamını
**üretir** (anahtar → tenant) ve onları `tappa_app` çağırır; `op_*` bağlamı bilerek
**aşar** ve onu yalnız operatör çağırır. md.7'nin beş kısıtından ikisi `op_*`'a uymaz ve
yerlerine geçen sınır aşağıda yazılıdır: (i) *"girdi yalnız anahtar"* → ilk parametre
**operatör oturum hash'i**, gerisi fonksiyonun adlandırılmış parametreleri; (ii) *"≤1
satır, sınırı bir UNIQUE indeks verir"* → çok satır dönen her `op_*` **LIMIT ≤ 200**.
Kalan üçü — sabit sütun listesi, çağıranın tablolarda doğrudan yetkisinin olmaması, naif
"bağlam `NULL` iken göster" dalının olmaması — aynen geçerlidir. md.7'nin kuralı da
aynen geçerlidir: **sayı değil sınır** — "zaten N tane `op_*` var" yeni bir `op_*` için
gerekçe değildir; her yenisi aşağıdaki sözleşmenin tamamını **ölçerek** yeniden kazanır.

## Bağlam — bugün (ölçüldü, dev Postgres 17.10, HEAD `f6f5a9b`)

| Olgu | Ölçüm |
|---|---|
| Roller: `tappa_app` (`rolsuper=f`, `rolbypassrls=f`), `tappa_owner` (`t`,`t`), `tappa_resolver` (`f`,`t`, NOLOGIN); `tappa_resolver`'ın üyesi 0 | `pg_roles`, `pg_auth_members` |
| Veritabanındaki `SECURITY DEFINER` fonksiyonlar: altı çözümleyici, **altısının** sahibi `tappa_resolver` (`rolsuper=f`); `proconfig` = `search_path=pg_catalog, pg_temp`; tablolar `public.`-nitelenmiş; PUBLIC `EXECUTE` = `f`, `tappa_app` `EXECUTE` = `t` | `pg_proc` (`prosecdef`), `has_function_privilege` |
| Yeni tablo `tappa_app`'e kendiliğinden açılır: dev'in `pg_default_acl`'i tablolar için `tappa_app=arwd` (üretim de geniş — backlog T46); **taze bir CI veritabanında** `01-roles.sql`'in daralttığı varsayılanla `ar`. Diziler için varsayılan `tappa_app=rU` | `pg_default_acl`; `scripts/db-init/01-roles.sql` |
| Her rol veritabanında `TEMP` hakkını PUBLIC'ten alır (`tappa_app` için `t`); `public` şemasında `CREATE` yok | `has_database_privilege` / `has_schema_privilege` |
| Havuz, üretimde superuser / `BYPASSRLS` / RLS'li bir tablonun sahibi / böyle bir rolün **üyesi** olan bir rolle açılmayı reddeder; geliştirmede uyarır | `internal/db/pool.go` `roleRefusal`, `RoleFacts.Privileged` (ADR 0017 §3.1) |
| Şemadaki bütün hash / anahtar-referansı sütunları (ve iki `bytea` sütunun ikisi): `tags.aes_key_ref`, `tags.app_key_ref`, `admin_users.password_hash`, `sessions.token_hash`, `admin_sessions.token_hash`, `password_resets.token_hash`, `employee_invites.code_hash` | `information_schema.columns` |
| Deponun süre deyimi `now()`'dır: `db/queries/tags.sql:584` (gerekçesiyle: *"now() is the TRANSACTION's start time"*), `db/queries/invites.sql:232,271` (`now() < expires_at`), `db/queries/sessions.sql:56` | kaynak |
| `now()` transaction başlangıcında **donar** (bir okuma transaction'ı 4 sn açık tutuldu: `now()` = 08:28:35.228, duvar saati 08:28:39.234) | §2 vii sondası |
| `idle_in_transaction_session_timeout`, `transaction_timeout`, `statement_timeout`: üçü de `context = user` (dev'de üçü de 0) — her oturum kendisi 0 yapar | `pg_settings` |
| `log_statement` ve `log_parameter_max_length`: `context = superuser` | `pg_settings` |
| `pg_xact_status` eski bir xid için `NULL` döndürür (`'100'::xid8` → `NULL`, `'3'::xid8` → `NULL`) | §2 v sondası |
| sqlc v1.28, `RETURNS TABLE(...)` döndüren bir fonksiyon çağrısını tipleyemiyor | ADR 0002 md.7 (üç ayrı ölçüm) — bu yüzden `internal/db/resolve.go` elle yazılı |

**Sondaların yöntemi.** Hepsi dev veritabanında `tappa_owner` oturumundan. Kalıcı
katalogda iz bırakabilecek her şey `BEGIN … ROLLBACK` içinde, geçici `zz_probe_*`
nesneleriyle. Çağıran, 2. turdan itibaren `SET SESSION AUTHORIZATION tappa_app` ile
**gerçek, üye olmayan, NOSUPERUSER** bir rol (sondada `rolsuper=f`, üyelik 0 ölçüldü);
definer'lar NOLOGIN, NOSUPERUSER, BYPASSRLS bir sondaj rolüne ait. (Superuser oturumda
`SET ROLE`'ün yanıltıcı olduğunu güvenlik denetimi ölçtü; 1. turun `search_path`
sondası da superuser sahipli fonksiyonla ve `SET ROLE` ile koşmuştu — okuma ve yazma
yarısı 2. turda NOSUPERUSER sahip ve gerçek çağıranla yeniden ölçüldü, §2 iv.)
**İstisna, adıyla:** commit gerektiren bilet sondaları (§2 v B satırları, §2 vii)
`BEGIN … ROLLBACK` ile ölçülemez; onlar oturuma özel **geçici nesnelerle** (`pg_temp`;
Postgres bağlantı kapanınca siler) koştu. Kalıcı bir tabloda koşan tek parça, commit
edilmemiş bir satırın başka bir oturumdan görünmediğini ölçen eşzamanlı iki oturum
sondasıdır (`legal_documents`'a `BEGIN … ROLLBACK` içinde bir satır). Her sondadan sonra
kalan `zz_probe_*` nesne, fonksiyon ve rol sayısı **0** (ölçüldü).

## Karar

### 1. Roller ve üç oturumsuz istisna

| Kim | Nitelik | Tutar | ASLA tutmaz |
|---|---|---|---|
| **`tappa_operator`** (rol) | NOSUPERUSER, **NOBYPASSRLS**, NOCREATEDB, NOCREATEROLE; hiçbir tablonun sahibi değil; **hiçbir rolün üyesi değil** | `op_*` üzerinde `EXECUTE`; `platform_admins`'te giriş araması için **yalnız `SELECT`**, dar sütun listesiyle (parola digest'i ve TOTP zarfı dahil — sınır 11; kesin liste OP-5) | hiçbir tenant tablosunda hiçbir yetki · **`platform_admins` üzerinde HİÇBİR yazma** (`INSERT`, `UPDATE`, `DELETE`) ve enrollment token hash'inde `SELECT` · **`platform_sessions`'ta hiçbir fiil** (oturum `op_open_session` ile doğar, `op_touch_session` ile yaşar, `op_close_session` ile biter) · **bilet tablosunda dört fiilin hiçbiri** · `operator_audit_log`'a doğrudan `INSERT` ve **`operator_audit_log` üzerinde `SELECT`** (görüntüleyici iki aşamalı `op_read_audit` ile okur — OP-14) · DEFAULT PRIVILEGE |
| **`tappa_opdefiner`** (rol) | **NOLOGIN**, **BYPASSRLS**, NOSUPERUSER, NOCREATEDB, NOCREATEROLE — `tappa_resolver`'ın şekli; **üyesi yok** | `op_*`'ların **sahibi**; fonksiyonların gerektirdiği tablolarda **SÜTUN düzeyi** yetkiler; bilet tablosunda `INSERT` **yalnız bağlama sütunlarında** (hash, oturum, tür, hedef, `audit_id`, `expires_at`) ve `UPDATE` **yalnız `consumed_at`**'te; `platform_admins`'te kimlik bilgisi, durum, TOTP adımı, son giriş ve kilit sayacı sütunlarında `UPDATE` (yazar, digest'i ve zarfı **okumaz**) · *(OP-12, 00032)* sahibi olmadığı fonksiyonlarda EXECUTE **yalnız** adlı bir listede: 00016'nın beş fatura fonksiyonu ("OP-12 uygulama notu" md. 10) | aşağıdaki "asla" sütunlarında `SELECT` · **`platform_admins` üzerinde `INSERT`** · bilet tablosunun **`created_at`, `created_xact`, `consumed_at`** sütunlarında `INSERT` (DEFAULT doldurur) ve `consumed_at` dışındaki sütunlarında `UPDATE` · `operator_audit_log`'un **zaman sütununda** `INSERT` (DEFAULT `clock_timestamp()` doldurur) · DEFAULT PRIVILEGE · LOGIN · üye |
| **`tappa_app`** (rol) | değişmez | **hiçbir yeni yetki** | yeni tablolarda ve dizilerde hiçbir fiil · `op_*` üzerinde `EXECUTE` |
| **`op_record_auth_event`** (oturumsuz istisna 1 — fonksiyon) | sahibi `tappa_opdefiner`, `EXECUTE` yalnız `tappa_operator`; **oturum parametresi YOK** | kapalı bir tür kümesinden **oturum öncesi** satır yazar: beş başarısızlık — `login_failed`, `unknown_email`, `totp_failed`, `locked`, `enrollment_failed` — ve *(OP-14, 00031)* `password_ok` (parolası kabul edilmiş, ikinci faktörü henüz tamamlanmamış girişin izi; hesabı **yalnız id ile** adlandırır, kilit sayacına dokunmaz); `RETURNS void` | aktör iddiası · tenant verisi / `tenant_id` · e-posta metni ya da e-postanın herhangi bir hash'i · oturum doğuran başarı satırı (`login`/`enrollment` — onlar `op_open_session`/`op_complete_enrollment`'ındır) · ~~başarı satırı~~ *(OP-14 notu: "yalnız başarısızlık" ve "başarı satırı" `password_ok` ile yanlışlandı; kesin hâl sol hücrede)* |
| **`op_open_session`** (oturumsuz istisna 2 — fonksiyon) | sahibi `tappa_opdefiner`, `EXECUTE` yalnız `tappa_operator`; **oturum parametresi YOK** (oturumu kendisi doğurur) | **tek ifadede**: TOTP adımını ilerletir (`totp_last_step < $adım` tekrar koruması **ve adımın duvar saatine bağlılığı** — `cur ± 1` — bu ifadededir), **kilit koşulunu** aynı `WHERE`'de sınar, son girişi yazar ve kilit sayacını sıfırlar, oturum satırını (`mfa_verified_at` dolu) ve köken satırını (`login`) yazar; yalnız `active` bir operatör için; `RETURNS void` | parola ya da TOTP doğrulaması (yapamaz — §1 açıklaması) · tenant verisi · duruma bağlı dönüş |
| **`op_complete_enrollment`** (oturumsuz istisna 3 — fonksiyon) | sahibi `tappa_opdefiner`, `EXECUTE` yalnız `tappa_operator`; **oturum parametresi YOK**; **ham** enrollment token'ını alır ve içeride hash'ler | **tek koşullu ifadede**: token'ı tüketir (tek kullanım, süre `clock_timestamp()` ile), parola digest'ini ve mühürlü TOTP sırrını yazar, TOTP adımının ilk değerini yazar (**`cur ± 1` ile sınırlı**), durumu `active` yapar, oturumu açar ve köken satırını (`enrollment`) yazar; yalnız `pending` bir hesap için; `RETURNS void` | parola ya da TOTP doğrulaması (yapamaz — Go'da) · digest'i ya da zarfı okumak · tenant verisi · duruma bağlı dönüş ya da hata ayrımı |

**`tappa_operator` havuzun ret kapısından geçer ve geçmeye devam etmelidir.**
`RoleFacts.Privileged()` bu rol için `false`'tur (BYPASSRLS yok, sahiplik yok, üyelik
yok). Biri ileride `tappa_operator`'ı `tappa_opdefiner`'ın üyesi yaparsa, `roleRefusal`
üretimde açılışı **reddeder** (*"member_of_a_superuser_or_bypassrls_role"*) — üyelik
yoluyla BYPASSRLS kazanmanın yapısal freni zaten kodda. `tappa_opdefiner`'ın üye sayısı
**0**'dır ve katalog testiyle tutulur (§6).

**`tappa_operator` NOLOGIN ve parolasız doğar**, `tappa_app`'in ölçülmüş fail-open
düzeltmesiyle aynı şekilde (`scripts/db-init/01-roles.sql` başlığı: parolayı veren
adım düşerse rol **hiç** giriş yapamamalı, repodaki bir parolayla yaşamamalı). ~~Giriş,
parolayı bir Secret'tan veren ayrı adımda açılır (§5).~~ → *(OP-7 notu, 2026-10-01: giriş
ayrı bir adımda açılır, ama parola bir Secret'tan **gelmez**: psql'in `\password`
komutuyla **stdin'den** verilir; ayrı bir Secret anahtarı yoktur, parola yalnız
`TAPPA_OPERATOR_DATABASE_URL`'in içinde durur — `deploy/README.md`, "Operator surface
(M10 OP-7)" runbook'u, 2. adım.)*

**`tappa_operator` oturum hash'lerini GÖREMEZ — oturumun bütün hayatı definer'lardadır.**
Ölçüldü: `WHERE`'de geçen bir sütun için Postgres sütun `SELECT`'i ister (yalnız
`SELECT (id)` tutan bir rolün `… WHERE token_hash = …` sorgusu `permission denied`).
Güvenlik denetimi bunun sonucunu ölçtü: touch ifadesi için `SELECT (id, token_hash)`
alan rol **bütün canlı hash'leri listeledi** — (i) `op_*`'a hash'i kimlik bilgisi olarak
verdiği için bu, başka bir operatörün oturumuyla `op_*` çağırmak demektir. **Karar:**
oturum `op_open_session` ile doğar, geçerlilik yüklemi **tek** bir tanımda,
`op_touch_session(p_session)`'da yaşar (§2 i), çıkış/iptal `op_close_session(p_session)`
ile olur ve çıkış satırını aynı ifadede yazar. `tappa_operator` `platform_sessions`'ta
hiçbir fiil tutmaz.

**`tappa_opdefiner` — ASLA okunmayan sütunlar (normatif).** Buradaki **"asla" bir
`SELECT` yasağıdır**: bu sütunlarda `tappa_opdefiner`'ın `SELECT` yetkisi yoktur ve hiçbir
`op_*` onları döndürmez. Yazma (`INSERT`/`UPDATE`) ayrı bir karardır ve istisnaları
adıyla sayılır (aşağıda; §3.3). Ayrım OP-18'de yük taşıyacaktır: operatör encode'u
`tags.aes_key_ref`/`app_key_ref`'i **yazmak** zorunda kalacak, bu kural ise onların
**okunmasını** yasaklamaya devam edecek.
- **Tenant tablolarının sır sütunlarının tamamı:** `tags.aes_key_ref`,
  `tags.app_key_ref`, `admin_users.password_hash`, `sessions.token_hash`,
  `admin_sessions.token_hash`, `password_resets.token_hash`,
  `employee_invites.code_hash`. (Liste bugünkü şemanın bu sınıftaki sütunlarının
  **tamamıdır** — yukarıdaki katalog ölçümü. Plan metnindeki beş ad —
  `aes_key_ref`, `app_key_ref`, `password_hash`, `token_hash`, `code_hash` — bu yedi
  sütunu adlandırır.)
- **Aynı sınıf platform tarafında:** `platform_admins`'in parola digest'i ve TOTP zarfı.
  `op_*` bunlara hiç ihtiyaç duymaz — oturumu çözer ve operatörün durumuna bakar.
- ⚠️ **İstisna, ADIYLA: `platform_sessions.token_hash`.** Oturumu fonksiyonun **kendisi**
  çözer, yani `WHERE token_hash = p_session`; `tappa_opdefiner` bu sütunu **görür**
  (yukarıdaki ölçüm), hiçbir `op_*` onu **döndürmez**.
- ⚠️ **Yazma istisnası, ADIYLA: `op_complete_enrollment`'ın digest ve zarf `UPDATE`'i.**
  Digest'i ve mühürlü sırrı **yazar** (`SET`), okumaz — *"asla"* (`SELECT`) kuralı
  ayaktadır ve katalogda pinlidir (§6).
- **`platform_admins`'e `INSERT` YOK** — ADR 0020 §6'nın *"yalnız `tappa_owner` INSERT
  eder"* kuralının DB yarısı; katalogda pinli (§6).

**`tappa_operator` `platform_admins`'e HİÇBİR ŞEY yazamaz (3. tur eki, orkestratör
kararı).** Hesabı değiştiren her şey — enrollment'ın tamamlanması, TOTP adımı, son giriş,
kilit sayacı — bir definer'dan geçer ve kendi satırını aynı ifadede yazar:
`op_complete_enrollment` (enrollment), `op_open_session` (TOTP adımı + son giriş + sayaç
sıfırlama + oturum), `op_record_auth_event` (`totp_failed` türünde sayacın artışı), ve
`tappa_owner`'ın `opadmin` SQL'i (`create`, `reset-mfa`, `disable` — ADR 0020 §6).
Kataloğa: `has_any_column_privilege('tappa_operator', 'platform_admins', 'UPDATE')` ve
`'INSERT'` = `false`, `has_table_privilege(…, 'DELETE')` = `false`. `tappa_operator`
giriş araması için digest'i ve zarfı **okur** — Go bcrypt'i ve TOTP'yi doğrular; kalan
riski sınır 11'dedir.

**`tappa_app` — açık `REVOKE ALL`, bütün yeni tablolarda.** Yukarıda ölçüldü: dev'de ve
üretimde bir migration'ın yarattığı tablo `tappa_app=arwd` ile, taze CI'da `ar` ile
doğar — ikisinde de açık. `REVOKE` yazılmazsa müşteri uygulamasının rolü operatörün
parola digest'ini, TOTP zarfını, oturum hash'lerini, operatör audit'ini ve okuma
biletlerini okur (ve dev/üretimde yazar). Kapsam **bu ADR'nin doğurduğu her tablodur**:
`platform_admins`, `platform_sessions`, `operator_audit_log` (adı `platform_` ile
başlamayan) ve okuma bileti tablosu (§2 v).
**Diziler:** bu tabloların hepsi **uuid** birincil anahtar kullanır (repo kalıbı); bir
gün dizi doğarsa (`bigserial` vb.) aynı migration `REVOKE ALL ON SEQUENCE … FROM
tappa_app` yazar — dizilerin varsayılanı `tappa_app=rU`'dur (ölçüldü) ve tablo
`REVOKE`'u diziyi kapsamaz (güvenlik denetimi: `tappa_app` bir `bigserial`'ın
`last_value`'sunu okudu).
Her `op_*` PUBLIC'ten `REVOKE ALL` alır (fonksiyonlar varsayılan olarak PUBLIC
`EXECUTE` ile doğar — 00003'ün yorumu) ve yalnız `tappa_operator`'a `EXECUTE` verilir.

**Ölçü sözleşmesi — "yetkisiz" nasıl kanıtlanır.** `SELECT`/`INSERT`/`UPDATE` için
`has_any_column_privilege`, `DELETE` için `has_table_privilege`. `has_table_privilege`
tek başına **sütun düzeyi grant'ı görmez** — ölçüldü, bugün `legal_documents`'ta:
`has_table_privilege('tappa_app', …, 'INSERT') = f` iken
`has_any_column_privilege(…) = t` ve `tappa_app` **yazabiliyor**. Tablo düzeyi sorgu
yeşil kalır ve hiçbir şey kanıtlamaz.

**Bilet tablosu `tappa_operator`'a DÖRT fiilde de kapalıdır — ölçülmüş iki sebep**
(güvenlik denetimi): `INSERT` tutan bir rol **audit'siz bir bilet basıp veri aldı**;
`UPDATE (consumed_at)` tutan bir rol **tüketimi sıfırladı**. Bileti yalnız
`op_begin_read` yazar, yalnız `op_read_*` tüketir.

**`op_record_auth_event` — neden var, neden dar, e-postaya ne olur, nasıl sınırlanır.**
ADR 0020 §5 oturum doğmadan önceki başarısızlıkları (başarısız giriş, bilinmeyen
e-posta, TOTP hatası, kilit, başarısız enrollment) `operator_audit_log`'a yazdırıyor;
sözleşmenin (i) maddesi ise her `op_*`'a oturum şart koşuyor. İki kolay çıkış da
reddedildi: `tappa_operator`'a doğrudan `INSERT` (DSN'i tutan her türlü satırı — başarı
dahil — basar) ve oturumsuz **genel** bir definer ((i) delinir). **Karar:** dar,
oturumsuz bir definer:
- **Kapalı tür kümesi** (yukarıdaki beş tür; şemada CHECK) ve **yalnız başarısızlık**.
  *(OP-14 notu, 2026-10-06 — bu madde 00031 ile yanlışlandı ve düzeltildi: küme altı türdür;
  altıncısı `password_ok` bir başarısızlık değil, **oturum doğurmayan** bir izdir — parola
  kabul edildi, ikinci faktör henüz gelmedi. Oturum öncesidir: aktör ve oturum taşımaz
  (`operator_audit_log_actor_shape`'in oturumsuz koluna aynı migration'da girdi), hesabı
  yalnız **id ile** adlandırır (adres verilirse 22023), ve kilit sayacına **dokunmaz**.
  "Başarı" — bir oturumun doğuşu — hâlâ yalnız `op_open_session`/`op_complete_enrollment`'ın
  köken satırıdır. Gerekçe ve sınır: "OP-14 uygulama notu", sınır 7.)*
- **Aktör iddia etmez.** Satır *"bu hesaba karşı bir deneme"* der, *"şu operatör yaptı"*
  demez.
- **E-posta saklanmaz, hash'i de saklanmaz.** Fonksiyon aldığı adresi
  `platform_admins`'te arar (`citext` eşitliği `OPERATOR(public.=)` ile — ADR 0002
  M6-01 tuzağı). Eşleşirse satır o hesabın **id**'sini *hedef hesap* olarak taşır;
  eşleşmezse (`unknown_email`) satır adres hakkında **hiçbir şey** taşımaz.
  *(OP-5 notu, 2026-09-26: "eşleşme" veritabanının araması, **tür** ise Go'nun görüşüdür.
  Go giriş aramasında RLS yüzünden yalnız `active` hesapları görür, yani `pending` ya da
  `disabled` bir hesabın adresini `unknown_email` diye bildirir — ve satır o hesabın id'sini
  taşır. "`unknown_email` + hedef" = giriş yapamayan bir hesabın adresi; "hedefsiz" = böyle
  bir hesap yok. `TestOpRecordAuthEvent_ClosedSetNoActorNoAddress` pinler.)* Gerekçe:
  bilinmeyen bir adres saldırganın yazdığı serbest metindir ya da gerçek bir kişinin
  yanlış yazılmış adresidir (kişisel veri; M7-06'nın *"The address is NOT logged"*
  pratiği, R7b); anahtarsız bir hash (örn. SHA-256) adres uzayında sözlükle geri
  çevrilir, yani takma ad değil gizlenmiş kişisel veridir; anahtarlı bir hash ise
  definer'a bir sır taşımayı gerektirir.
- **`RETURNS void`** — adresin bir operatöre ait olup olmadığını çağırana **dönüş
  değeriyle** söylemez. *(Düzeltme, OP-5 4. tur, 2026-09-26: "fonksiyon bir üyelik
  kehaneti değildir" cümlesi fazlaydı. Herkese açık istatistik görünümleri eşleşmeyi
  ele verir — `pg_stat_xact_user_tables` bir eşleşmenin FK kontrolünü sayar; sayılı
  sınır 15, ölçümüyle.)*
- `tenant_id` yok; hiçbir tenant verisine dokunmaz; yalnız `operator_audit_log`'a yazar.
- 🔴 **İnternetten tetiklenen bir yazma kapısıdır, bu yüzden sınırlıdır (normatif).**
  Panel bugün bilinmeyen e-postayı veritabanına **yazmıyor**, yalnız süreç log'una
  (`internal/handler/adminlogin.go:1232`), ve deponun kendi dersi aynı dosyada:
  *"an unbounded row here would be a write primitive into an append-only table"*
  (`adminlogin.go:1300-1301`). Buna göre: (a) oturum öncesi satırlar **IP'den bağımsız,
  süreç genelinde bir tavanla** sınırlanır (sayısı OP-6/OP-8; ilke normatif); (b)
  **limiter'ın reddettiği istek satır YAZMAZ**; (c) 🔴 **TOTP kilidi audit satırlarından
  TÜRETİLMEZ** — kilit kararı satırları saymaz, `platform_admins` üzerindeki hesabın kendi
  sayacını okur; böylece limiter'ın geçirmediği ya da başka türden bir satır kilide
  dönüşmez ve sayaç başarıda sıfırlanabilir. (3. tur eki: sayacın kendisini hangi yolun
  taşıdığı ve bunun DSN sahibine karşı neden korunamadığı hemen aşağıda.) Tavan Go
  tarafındadır; DSN sahibi onu atlar (sınır 7).
  **OP-6 düzeltmesi (2026-09-26), ölçümle — (a) ile (c) birbirini bozabiliyordu:** (a)
  bütün beş türe TEK bir ortak tavan olarak okunursa parolasız çöp tavanı doldurur,
  `totp_failed` susar ve (c)'nin sayacı durur: kilit kapanır. Ölçüldü (Go'da, o
  yönlendirmeyle): tavan tükenmişken 5 yanlış kod → **0** satır, sayaç **0**. Bu yüzden
  ortak süreç tavanı (30 / 10 dk) yalnız **parolasız** türleri (`unknown_email`,
  `login_failed`, `enrollment_failed`) kapsar; `totp_failed` ve `locked` yalnız doğru
  parolayla basılmış bir giriş challenge'ını tutanın harcayabildiği, kod denetlenmeden ÖNCE
  harcanan hesap bütçesiyle (10 / 10 dk / operatör) × aktif operatör sayısıyla sınırlıdır.
  İkisi de IP'den bağımsızdır. Bu, TOTP adımının **iki** kolunu da kapsar: Go'nun reddettiği
  yanlış kod (`totp_failed`) ve Go'nun kabul edip veritabanının reddettiği kod — tekrar
  edilen kod (`totp_failed`, sayacı ilerletir) ve kilitliyken doğru kod (`locked`); 2026-09-30
  doğrulamasında ikinci kolun tavandan geçirilmesi pinsiz bulundu (mutasyon yeşil kaldı) ve
  aynı teste bağlandı. [m10-platform.md](../plan/m10-platform.md) → OP-6 kart düzeltmesi,
  md. 8–9.
- **Kilit sayacının yolu (3. tur eki):** `tappa_operator` sayaca yazamadığı için sayacı
  bir definer taşır, ve üç adlı kümeye dördüncü bir oturumsuz ad eklememek için o definer
  **bu fonksiyondur**: `totp_failed` türü, satırı yazdığı **aynı ifadede** hesabın
  sayacını bir artırır; `op_open_session` başarıda sıfırlar. Kilit kararı **audit
  satırlarını saymaz** — hesabın sayacını okur, ve bu okuma **`op_open_session`'ın AYNI
  `UPDATE`'inin `WHERE`'indedir** (sayaç eşiğin altında ya da varsa kilit penceresi
  `clock_timestamp()`'e göre geçmiş); oku-sonra-karar Go'da kalmaz (eşik ve pencere
  OP-6/OP-8). İnternetten
  gelen biri için `totp_failed` ancak parola adımı geçildikten sonra (ara çerez) ve
  limiter'ın izin verdiği kadar doğar. ⚠️ **DSN sahibine karşı kilit korunamaz:** sayaca
  giden her yol `tappa_operator`'ın çağırabildiği bir definer'dır, yani sahte
  `totp_failed` basan DSN sahibi bir hesabı kilitleyebilir — sınır 7'de sayılı; her
  artış bir audit satırıyla birlikte doğar.

**`op_open_session` — oturumu bir definer doğurur ve kökenini aynı ifadede yazar.**
Parola ve TOTP doğrulaması süreçte yapılır (TOTP KEK'i süreçte; ADR 0020 §3), yani
definer bir oturumun **hak edilip edilmediğini doğrulayamaz** — çağırana güvenir. Bu
yeni bir güç **eklemez**: `tappa_operator` DSN'ini tutan birinin oturum basabilmesi zaten
sınır 1'dir. Eklediği şey **izdir**: oturum satırı ve köken satırı **aynı SQL ifadesinde**
(veri değiştiren bir CTE ile) yazılır, dolayısıyla **var olan her oturumun commit edilmiş
bir köken satırı vardır** — biri geri alınırsa ikisi birlikte gider. Kısıtları:
- Yalnız `status = active` bir operatör için oturum doğurur; oturum `mfa_verified_at`
  dolu doğar (MFA damgası sürecin beyanıdır — sınır 1).
- **TOTP adımı aynı ifadede ilerler:** `op_open_session(p_admin, p_session_hash,
  p_totp_step)` hesabın satırını `… WHERE id = p_admin AND status = 'active' AND
  totp_last_step < p_totp_step AND p_totp_step BETWEEN cur - 1 AND cur + 1 AND
  <kilit koşulu>` ile günceller — `cur = floor(date_part('epoch', clock_timestamp()) /
  30)::bigint` — (`totp_last_step`, son giriş, kilit sayacının sıfırlanması) ve oturumu
  ve köken satırını o güncellemenin döndürdüğü satırdan doğurur; **0 satır =
  EXCEPTION**, oturum yok, satır yok.
- 🔴 **Adım duvar saatine bağlıdır (4. tur, güvenlik denetimi).** Bağsız hâli bir
  **kalıcı kilit ilkeli** idi — denetçi ölçtü: `op_open_session(…,
  9223372036854775807)` kabul edildi; ardından meşru adım da, bir yıl sonraki adım da
  reddedildi. Aynı yüklem bu turda yeniden ölçüldü (`cur` = o anki adım):

  | Verilen adım | `cur-1 … cur+1` içinde mi |
  |---|---|
  | `9223372036854775807` (zehir) | hayır |
  | `cur-2` · `cur+2` · `cur` + bir yıl | hayır |
  | `cur-1` · `cur` · `cur+1` | **evet** |

  Go ve veritabanı saatlerinin ±1 adım (≈30 sn) içinde anlaşması gerekir; anlaşmazlarsa
  geçerli kodlar reddedilir — yön fail-closed'dır (sınır 13). Tekrar koruması
  (ADR 0020 §1, §4.4'ün aynası) böylece oturumun doğuşuyla **ayrılamaz**: tekrar edilen
  bir kod oturum açamaz. Ayrı bir adım-ilerletme definer'ı elendi (dördüncü oturumsuz ad
  ve korumayla doğuşun iki ifadeye bölünmesi).
- Köken satırının türü `login`'dir. Enrollment'ın kökeni `op_complete_enrollment`'ın
  kendi satırıdır.
- `RETURNS void` (§2 v yazma kuralı); oturumun kimliğini sonraki çağrılarda
  `op_touch_session` döndürür.
- **Çıkış satırı** aynı ilkeyle: `op_close_session(p_session)` iptali ve çıkış satırını
  aynı ifadede yazar (oturum taşıyan sıradan bir yazma).

**`op_complete_enrollment` — enrollment'ı ve ilk oturumu tek koşullu ifadede bitirir
(3. tur eki, orkestratör kararı; sınır 10'u kapatır).**
- **Girdi:** hesabın id'si, **ham** enrollment token'ı (içeride anahtarsız SHA-256 ile
  hash'lenir — ADR 0020 §3; saklanan hash kimlik bilgisi değildir), Go'nun hesapladığı
  bcrypt digest'i, Go'nun mühürlediği TOTP sırrı, ilk kodun kabul edildiği TOTP adımı ve
  yeni oturumun token hash'i.
- **Tek koşullu ifade** (veri değiştiren bir CTE): `platform_admins`'te `… WHERE id =
  p_admin AND status = 'pending' AND enroll_token_hash = <hash> AND enroll_used_at IS
  NULL AND enroll_expires_at > clock_timestamp() AND p_totp_step BETWEEN cur - 1 AND
  cur + 1` (adım bağı `op_open_session`'daki gibi) → token tüketilir, digest ve zarf,
  `totp_last_step` = verilen adım yazılır, durum `active` olur; dönen satırdan oturum
  (`mfa_verified_at` dolu) ve köken satırı (`enrollment`) doğar. **0 satır = EXCEPTION**,
  nedeni ayrılmadan (süresi dolmuş / kullanılmış / yanlış eşleşme aynı hata — §2 v 7).
- **Id neden girdi:** zarfın AAD'si `platform_admins.id`'dir (ADR 0020 §1), yani Go
  mühürlemeden **önce** id'yi bilmek zorundadır; `tappa_operator` enrollment hash'ini
  okuyamaz ve bir arama definer'ı dördüncü oturumsuz ad olurdu. Bu yüzden
  `opadmin create` id'yi kendisi üretir ve enrollment linkine token'la birlikte koyar
  (ADR 0020 §6); id bir sır değildir, eşleşme **id ve token hash'i birlikte** aranır.
- **Doğrulamanın yeri:** ilk kodun doğrulaması Go'dadır (sır ve KEK orada): Go sırrı
  üretir, gösterir, ilk kodu RFC 6238 ±1 ile doğrular, kabul edilen adımı bu çağrıya
  verir. Adımın `totp_last_step` olarak yazılması, enrollment kodunun ilk girişte
  **tekrar oynatılamamasını** sağlar (`op_open_session` adımın büyük olmasını ister).
  Definer bir enrollment'ın hak edilip edilmediğini doğrulayamaz — ama ham token'ı
  ister, yani DSN sahibi de ancak linki elinde tutan bir enrollment'ı bitirebilir.
- **Eşzamanlılık:** aynı token'la N eşzamanlı çağrı → **tam 1** başarı (koşullu tek
  ifade; emsal `TestConsumeInvite_ConcurrentRaceExactlyOneWinner`,
  `internal/db/invites_test.go:213`).
- **Kimliksiz son adımın bedeli (sınır 12):** `POST /operator/enroll`, token veritabanında
  doğrulanmadan önce Go'ya bir cost-12 bcrypt ve bir `Seal` ödetir — `tappa_operator`
  token'ı önceden kontrol edemez. ADR 0015'in *"Consume … tam bir cost-12 bcrypt öder"*
  emsaliyle çare bir **oran sınırıdır** (adres başına + süreç geneli; sayılar OP-8).

**Giriş araması** (e-posta → digest + zarf, `resolve_admin_by_email` emsali) **öneri
olarak kalır** — bugünkü kararla `tappa_operator` bu iki sütunu `SELECT` eder, çünkü Go
bcrypt'i ve TOTP'yi doğrular (kalan risk: sınır 11). **Benimsenirse şu kurallar
güncellenir:** (1) `tappa_opdefiner`'ın *"digest/zarf okumaz"* kuralı çiğnenmesin diye
arama **ayrı** bir definer rolüne verilir ve `prosecdef` sahip kümesi (§6) o rolü içerecek
şekilde büyür; (2) oturumsuz istisna kümesi **dördüncü** bir ad alır; (3)
`tappa_operator` `platform_admins`'te digest'i ve zarfı `SELECT` etmeyi kaybeder.

### 2. `op_*` sözleşmesi

**(i) İlk parametre operatör oturumunun token HASH'idir; aktör beyan edilemez.**
Oturum yüklemi **tek** bir tanımdadır, `op_touch_session(p_session)`: süresi dolmamış
(mutlak 8 sa + boşta 30 dk, **duvar saatiyle** — vii), `mfa_verified_at` dolu,
`revoked_at` boş, operatörü `active` — ADR 0020 §2 — ve `last_used_at`'i aynı ifadede
ilerletir. Go'nun oturum kapısı onu çağırır; **her diğer `op_*` gövdesinde onu çağırır**
(ikisi bir yüklemin iki kopyası değil, aynı fonksiyondur). Saat kaynağı tektir:
yüklemi koşan veritabanının duvar saati; OP-6'nın *"saat enjekte DB testleri"* kabulü,
satırın zaman damgalarını geriye yazarak karşılanır. Oturum geçersizse **EXCEPTION**.
Aktör (`platform_admins.id`) bu çözümden **türetilir**; hiçbir `op_*` bir aktör
parametresi almaz. **Üç oturumsuz istisna, adıyla:** `op_record_auth_event`,
`op_open_session` ve `op_complete_enrollment` (§1). Sınırı: sınır 1.

**(ii) Sabit sütun listesi.** `RETURNS TABLE(...)` açık yazılır; `SELECT *` yüzeyi yok;
§1'in "asla" sütunlarından hiçbiri dönüşte yok.

**(iii) LIMIT ≤ 200.** Çok satır dönen her `op_*`'ın gövdesinde tavan sabittir;
çağıranın verdiği sayfa boyu bu tavanla kırpılır, onu aşamaz.

**(iv) `SET search_path = pg_catalog, pg_temp`, her `public` nesnesi nitelenmiş,
dinamik SQL yok.** Tablolar `public.` ile yazılır; `citext` eşitliği
`OPERATOR(public.=)` ile (ADR 0002'nin M6-01 notundaki ölçülmüş tuzak: nitelenmemiş `=`
sessizce büyük/küçük harfe duyarlı olur). `EXECUTE`/`format()` ile kurulan SQL yok.

🔴 **Plan metni `SET search_path = pg_catalog, public` diyordu ve o biçim AÇIKTI —
ölçüldü.** Postgres, yolda **yazılmamış** `pg_temp`'i **ilk** arar. Aynı gövde, çağıranın
kendi oturumunda açtığı aynı adlı bir geçici tabloya karşı:

| `search_path` | Gövdedeki ad | 1. tur (superuser sahip): okunan / audit `INSERT`'i | 2. tur (NOSUPERUSER BYPASSRLS sahip, çağıran `tappa_app`): okunan | 2. tur: tek aşamalı yazma — gerçek değişiklik / gerçek audit / çağıranın geçici audit'i |
|---|---|---|---|---|
| `pg_catalog, public` | nitelenmemiş | **çağıranın geçici tablosu** / **çağıranın geçici tablosuna** | çağıran tabloyu definer'a `GRANT` etmeden: `permission denied`; `GRANT` ettikten sonra: **`forged-by-caller`** | **1 / 0 / 1** |
| `pg_catalog, pg_temp` | `public.`-nitelenmiş | gerçek tablo / gerçek tabloya | iki durumda da **`real`** | 1 / 1 / 0 |
| `pg_catalog, public, pg_temp` | nitelenmemiş | gerçek tablo / — (koşulmadı) | — (koşulmadı) | — (koşulmadı) |

**Açığın gerçek fiyatı.** Tek aşamalı bir **yazmada** (§2 v) değişiklik gerçek tabloya
commit edilir, audit satırı ise çağıranın geçici tablosuna gider ve bağlantıyla birlikte
yok olur — yani **audit'siz, commit edilmiş bir yazma** (tablonun son sütunu: değişiklik
1, gerçek audit 0). Okumada bilet ve audit birlikte saparsa veri, commit edilmiş bir
audit olmadan döner. **Seçilen: `pg_catalog, pg_temp` + nitelenmiş adlar** — altı
çözümleyicinin altısında kullanılan, testleri olan emsal. Üçüncü biçim de ölçümde
güvenliydi; seçilmedi, çünkü güvenliği `public`'e hiçbir çağıranın `CREATE` edemeyeceği
varsayımına dayanır (bugün doğru, ölçüldü) ve emsalden sapar. Seçilen biçimde
nitelenmemiş bir ad — çağıranın aynı adlı geçici tablosu yokken — gürültülü düşer
(ölçüldü: `relation "…" does not exist`).

**(v) Audit — yazmalar TEK aşamalı, okumalar İKİ aşamalı.**

🔴 **Tek aşamalı okuma izsiz okumaya izin veriyordu (güvenlik denetimi, 2. tur RED).**
Audit satırı çağıranın transaction'ında yazılır ve fonksiyon veriyi commit'ten **önce**
döndürür. `tappa_operator` DSN'ini tutan biri (RCE ya da sızan Secret — A-0'ın sınıfı):
`SAVEPOINT s; SELECT * FROM op_tenant_detail(<gerçek hash>, <B>); ROLLBACK TO SAVEPOINT s;`
→ B'nin verisi elinde, operatör log'unda **0 satır**. Denetçi bunu BYPASSRLS / NOLOGIN /
NOSUPERUSER bir sahip ve yalnız `EXECUTE` tutan bir çağıranla ölçtü. *"Sessiz bir
çapraz-tenant okuma yok"* (M9-08 kabul 2) bu çağırana karşı tutmuyordu.

**Karar (orkestratör, 2026-09-26): önce audit commit edilir, sonra veri döner.**
1. **`op_begin_read(p_session, p_kind, <okumanın bütün parametreleri>)`** kendi
   transaction'ında koşar ve **commit edilir**: oturumu `op_touch_session` ile çözer,
   aktörü türetir, `operator_audit_log`'a satırı yazar ve tek kullanımlık, kısa ömürlü
   bir **bilet** döndürür. Bilet satırı:
   - bileti **oturuma, türe ve okumanın BÜTÜN parametrelerine** bağlar — hedef tenant,
     sayfa, imleç, arama terimi (güvenlik denetimi: aksi hâlde tek bir audit satırı
     bütün sayfaları örter). 🔴 **Bağlama hash'in İÇİNDEDİR (4. tur, B7):** saklanan
     değer `sha256(ham bilet ‖ parametrelerin kanonik jsonb metni)`'dir. Ham bilet 256
     bitlik rastgele bir değerdir ve **hiçbir yerde saklanmaz**, dolayısıyla saklanan hash
     terim hakkında **hiçbir şey** söylemez — terimin tek başına hash'i ise (ADR'nin
     kendi e-posta gerekçesiyle) sözlükle geri çevrilir. Ölçüldü (`BEGIN … ROLLBACK`):
     aynı ham bilet + aynı terim → 1 eşleşme; aynı ham bilet + başka terim → 0; başka
     sayfa → 0; terimin tek başına hash'i → 0; jsonb metni anahtar sırasından bağımsız
     (`t`). **Arama terimi audit satırına YAZILMAZ — ne ham ne hash'i;** audit satırı
     *"arama yapıldı"* + hedef kapsamı + **opak** sayfa bilgisi taşır. 🔴 **İmleç de
     terim gibidir (kapanış kontrolü, 2026-09-26):** keyset bir imleç sonuç satırından
     türer (son satırın adı ya da e-postası), yani audit'e yazılırsa append-only tabloya
     kişisel veri ve aramanın sonucu girer. Audit'e yalnız **içeriksiz** sayfa bilgisi
     (sayfa numarası ya da sayfa boyu) yazılır; tam imleç, terim gibi yalnız bilet
     hash'inde bağlanır. *(Brief'in önerdiği "sonuç
     sayısı sınıfı" audit'e giremez: audit satırı sorgu koşmadan **önce** commit
     edilir — iki aşamalı okumanın bütün amacı budur.)* Ham bilet yalnız çağırana döner;
   - onu yaratan **üst düzey transaction'ın kimliğini** kaydeder:
     `created_xact xid8 NOT NULL DEFAULT pg_current_xact_id()`. 🔴 **`created_xact`,
     `created_at` ve `consumed_at` `tappa_opdefiner`'ın INSERT listesinde YOKTUR —
     DEFAULT doldurur** (ADR 0015 §2 emsali: yazılabilir bir zaman sütunu tavanı
     anlamsızlaştırır). Güvenlik denetimi ölçtü: geniş INSERT'li bir definer
     `created_xact = '2'` ve `created_at = +1 yıl` yazınca bilet **aynı transaction'da**
     veri verdi; dar INSERT `42501` verdi. Savunma derinliği olarak şemada
     `CHECK (created_xact > '2'::xid8)` (4. madde);
   - 🔴 **`audit_id uuid NOT NULL REFERENCES operator_audit_log`** taşır — yapısal bağ:
     bir `EXCEPTION` bloğu audit `INSERT`'ini düşürse bile bilet **var olamaz**;
   - **ömrü en çok 60 saniyedir**: `expires_at = clock_timestamp() + ömür`, şemada
     `expires_at <= created_at + interval '60 seconds'` CHECK'i (ADR 0015'in şema süre
     tavanı emsali; `created_at` `DEFAULT clock_timestamp()` ve yazılamaz).
2. **`op_read_*(p_session, p_ticket, <aynı parametreler>)`** **ham** bileti alır ve
   hash'i **içeride, aldığı parametrelerle birlikte** yeniden hesaplar — `token_hash`
   gerekçesinin aynısı: saklanan değer bir kimlik bilgisi olmamalı, onu okuyabilen onu
   kullanamamalı; ve başka parametrelerle gelen bilet eşleşmez. Bileti **tek bir tüketen
   `UPDATE`** ile doğrular ve tüketir; koşulların **hepsi o `UPDATE`'in `WHERE`'inde ve
   POZİTİF biçimde** durur: hash (bilet + parametreler) eşleşiyor · oturum/tür eşleşiyor ·
   `consumed_at IS NULL` · `expires_at > clock_timestamp()` ·
   `pg_xact_status(created_xact) = 'committed'`. `UPDATE` yalnız `consumed_at`'i yazar;
   0 satır = EXCEPTION.
3. 🔴 **`xmin` kontrolü AŞILIYOR — ölçüldü (2. tur).** Bilet aynı transaction'ın bir
   **savepoint**'i içinde yaratılırsa satırın `xmin`'i bir **alt-transaction**
   kimliğidir, üst düzey kimlikle eşleşmez ve kontrol veriyi verir.
   `pg_current_xact_id()` ise savepoint içinde de **üst düzey** kimliği döndürür
   (ölçüldü) — bu yüzden kimlik satıra açıkça yazılır ve durumuna bakılır:

   | Sonda | `xmin ≠ üst düzey xid` kontrolü | `pg_xact_status(created_xact) = 'committed'` kontrolü |
   |---|---|---|
   | A1 — bilet bu transaction'ın **üst düzeyinde** yaratıldı, aynı transaction'da kullanıldı | ret | ret |
   | A2 — bilet bu transaction'ın bir **savepoint**'inde yaratıldı, aynı transaction'da kullanıldı | 🔴 **veri döndü** (`xmin` = alt-transaction kimliği) | ret (`created_xact` üst düzey, durumu `in progress`) |
   | B1 — bilet yaratıldı ve **COMMIT**; sonraki bir transaction'da kullanıldı | — | **veri** |
   | B1 — o okuma transaction'ı **ROLLBACK** edildi | — | audit satırı **kalıcı** (1 satır; commit edilmişti) |
   | B2 — bilet yaratıldı, transaction **ROLLBACK** edildi, sonra kullanıldı | — | ret; audit satırı da yok (veri hiç dönmedi) |
   | D — commit edilmemiş bir satır, eşzamanlı **başka bir oturumdan** | — | görünmez (0; kendi oturumu 1 gördü) |

   (A1'in ilk koşusu, bir `ROLLBACK TO SAVEPOINT`'ten sonra savepoint'in açık kaldığını
   gözden kaçırdığı için bileti farkında olmadan bir alt-transaction'da yarattı ve
   `xmin` kontrolünü **geçti** — aynı açığın kazara bir kanıtı. Tablo, her ret sonrası
   `RELEASE` eden ikinci koşudur.)
4. 🔴 **`pg_xact_status` koşulu POZİTİF yazılır — ve tek başına yetmez.** Ölçüldü:
   eski bir xid için `pg_xact_status` **`NULL`** döndürür (`'100'::xid8` ve `'3'::xid8`
   → `NULL`) ve `NULL <> 'committed'` de **`NULL`**'dür — `IF pg_xact_status(…) <>
   'committed' THEN RAISE` biçimi `NULL`'da **dalı atlar**, yani **fail-open**'dır.
   `WHERE … AND pg_xact_status(created_xact) = 'committed'` ise `NULL`'da satırı
   eşleştirmez → 0 satır → ret. Normatif: bu koşul (ve bilet koşullarının hepsi)
   tüketen `UPDATE`'in `WHERE`'indedir, ayrı bir `IF`'te değil. ⚠️ **Ama pozitif biçim
   de özel xid'lerde geçer:** `pg_xact_status('1'::xid8)` ve `('2'::xid8)` **`committed`**
   döndürür (güvenlik denetimi ölçtü; bu turda yeniden ölçüldü). Yani `created_xact`
   yazılabilir olsaydı `'2'` yazan bir definer aynı transaction'da veri alırdı. Koruyan
   şey `created_xact`'ın **yazılamaması**dır (1. madde) ve şemadaki
   `CHECK (created_xact > '2'::xid8)` savunma derinliğidir.
5. **Sonuç:** bir `op_read_*` veri döndürdüyse, o okumanın audit satırı **commit
   edilmiştir** ve commit edilmiş bir transaction geri alınamaz. Bu, *"sessiz bir
   çapraz-tenant okuma yok"*u DSN sahibine karşı da tutan yapıdır. Sınırları: sınır 3–5.
6. **Hangi `op_*` iki aşamalı:** veri döndüren **her** `op_*`; adlandırılmış iki
   istisna: `op_touch_session` (oturumun kendisi) ve `op_begin_read` (bilet). Kural tek
   biçimlidir — yasal sürüm listesi de dahil — ki denetim her fonksiyon için "bu tenant
   verisi mi" yargısına kalmasın. İkinci yarı `op_read_` önekini taşır (OP-11 kartındaki
   `op_list_tenants` / `op_tenant_detail` adları bu önekle ve ikinci argümanda biletle
   okunur).
7. **Yazmalar tek aşamalı kalır:** audit + değişiklik aynı transaction'da; geri alınırsa
   ikisi birlikte gider. 🔴 **Bu yalnız yazma hiçbir şey SIZDIRMAZSA doğrudur** —
   güvenlik denetimi ölçtü: `SAVEPOINT` + `disable_admins('B')` → **3** (B'nin yönetici
   sayısı) + `ROLLBACK TO` → audit yok; yani bir sayı bile izsiz bir **durum kehanetidir**.
   **Normatif:** bir yazma fonksiyonu **yalnız `void`** ya da yarattığı satırın kimliğini
   (**`uuid`**) döndürür. Etkilenen satır sayısı, *"değişmedi"*, *"zaten askıda"* gibi
   **önceden var olan duruma bağlı hiçbir dönüş ya da hata ayrımı** yoktur; sonucu
   görmek isteyen iki aşamalı bir okuma yapar. Katalogda kısmen pinlidir (§6).
   **Kapsamı:** kural **tenant durumunu değiştiren** yazmalar içindir. Oturumsuz üç
   fonksiyonun ve `op_close_session`'ın hataları yalnız ya `tappa_operator`'ın zaten
   okuduğu `platform_admins` verisini ya da ham token/hash sahibine **kendi** durumunu
   verir.
   **Düzeltme (2026-09-26, OP-5 2. tur — bu cümle `op_open_session` için eksikti):**
   `op_open_session` bir `SAVEPOINT` içinde çağrılıp geri alınırsa iz bırakmaz ama
   sonucu iki şey söyler: hesabın `totp_last_step`'inin `cur-1`/`cur`/`cur+1`'e göre
   yerini (yani bu adımda bir kodun kullanılıp kullanılmadığını) — `tappa_operator`'ın
   **SELECT edemediği** tek yarı budur — ve kilidin açık olup olmadığını; ikinci yarıyı
   `tappa_operator` zaten **doğrudan** okur (`totp_locked_until` giriş aramasının sütun
   listesinde; 4. turda ölçüldü: 5 `totp_failed` sonrası operatörün kendi SELECT'i kilidi
   okudu, `totp_last_step` SELECT'i *permission denied*). Kapatılmadı:
   fonksiyon adımı ilerletmek **zorunda** ve sonucu `void`/istisnadan başka bir şey
   değil; DSN sahibi zaten oturum basabilir (sınır 1). Sayılı sınır 14.
   **Var olmayan hedef (4. tur, B14):** `audit_log.tenant_id`'nin FK'si var olmayan bir
   tenant'a yazılan K6 satırını `23503` ile düşürür — bu, geri alınınca iz bırakmayan
   bir **varlık kehanetidir**. Normatif: tenant satırı `INSERT … SELECT … WHERE EXISTS`
   biçiminde koşulludur; var olmayan hedefe yazma **aynı `void`**'u döndürür, yalnız
   operatör satırını yazar ve hiçbir tenant'ı değiştirmez (§6 davranış testi).
8. **Her `op_*` yazar, dolayısıyla VOLATILE olmak zorundadır.** Ölçüldü: `STABLE` bir
   fonksiyondaki `INSERT` çalışma anında `INSERT is not allowed in a non-volatile
   function` ile düşer — çözümleyici şablonundaki (`STABLE`) kopyalanırsa gürültülü
   kırılır.
9. ⚠️ **Reddedilen çağrı veritabanında iz bırakmaz.** Ölçüldü: önce audit `INSERT`'i
   yapan, sonra `RAISE EXCEPTION` eden bir definer'dan sonra audit tablosunda **0
   satır**. `op_begin_read`'in ya da bir yazmanın reddi (geçersiz/süresi geçmiş oturum)
   çağıranın **kendi** transaction'ındadır ve o transaction DSN sahibinin elindedir.
   Sınır 3.

**Audit satırının zorunlu içeriği (normatif; sütun adları OP-5):** her
`operator_audit_log` satırı **oturum kimliğini** (oturumlu satırlarda), **türü** ve
**hedefi** (tenant ve, okumalarda, hedef kapsamı + içeriksiz sayfa bilgisi; arama terimi
ve imleç **hariç** — ne ham ne hash'i, §2 v 1) taşır; zaman sütunu `DEFAULT clock_timestamp()`'tir ve INSERT
listesinde yoktur (vii). Sınır 1 ve 4'ün savunusu bu değerlere dayanır.

**(vi) Tek yerde yazılır.** Her `op_*`'ın tanımı, `OWNER TO tappa_opdefiner`'ı ve
`REVOKE`/`GRANT`'ı **aynı migration'da** doğar (yarım yetkili bir ara durum yok). Bütün
`op_*`'ların kanonik SQL'i `db/queries/operator.sql`'dedir — `-- name:` taşımayan bir
belge, sqlc onu atlar (`db/queries/resolve.sql` emsali). Go erişimcileri elle,
`internal/db/operator.go`'da (`internal/db/resolve.go` emsali; sqlc bu çağrıları
tipleyemiyor).

**(vii) 🔴 Zaman `clock_timestamp()`'ten gelir; DONAN saatlerin HEPSİ bu fonksiyonlarda
YASAK.** `op_*` içindeki **her** süre, ömür ve boşta-süre karşılaştırması — bilet ömrü,
oturumun 8 saatlik mutlak ve 30 dakikalık boşta süresi, enrollment token süresi, TOTP
adımının bağı, kilit penceresi — **ve `op_*`'ın yazdığı her zaman damgası**
`clock_timestamp()`'tir. Yasak, adlarıyla: **`now()`, `CURRENT_TIMESTAMP`,
`transaction_timestamp()`, `statement_timestamp()`, `LOCALTIMESTAMP`, `LOCALTIME`,
`CURRENT_TIME`, `CURRENT_DATE`** ve **`'now'`/`'today'` gibi özel zaman literalleri**
(`'now'::timestamptz = now()` — kapanış kontrolünde ölçüldü, 1,2 sn geride kaldı) — hepsi
transaction ya da ifade başında donar ve bu tehdit modelinde transaction ve ifade
sınırlarını **DSN sahibi** seçer. Katalog taraması adları **kelime sınırıyla**
(`\mnow\M` biçiminde) arar; parantezli biçime (`now()`) bağlı bir desen literali kaçırır.
**Kural yeni değil, emsali var:** deponun genel deyimi `now()`'dır (yukarıdaki tablo) ve
`tags.sql:584` onu bilerek seçer; ama zamanın bir bekleyişe ya da başka bir transaction'a
karşı ölçüldüğü yerde depo zaten `clock_timestamp()`'i seçmiş —
`db/queries/transactions.sql:163-164` (*"clock_timestamp(), NOT now(): now() is the
TRANSACTION START time"*) ve ADR 0006'nın sunucu bacağı (:109-113, :189). Bu ADR o
emsali, zaman sınırlarını saldırganın seçtiği her yere genişletir.

Ölçüldü (güvenlik denetimlerinin ölçümlerinin yeniden üretimi) — ömrü 3 sn'lik bir bilet
commit edildi, okuma transaction'ı biletin ömrü dolmadan açıldı ve 4 sn açık tutuldu
(`now()` 08:28:35.228'de kaldı, duvar saati 08:28:39.234); 4. turda uyku **tek bir `DO`
ifadesinin İÇİNE** alındı:

| Deneme (bilet duvar saatine göre dolmuş) | `clock_timestamp()` | `now()` | `statement_timestamp()` |
|---|---|---|---|
| savepoint içinde çağrı + `ROLLBACK TO SAVEPOINT`, üç kez | ret | **3 / 3 veri** | — (güvenlik denetimi: tarif edilen iki testi **geçti**) |
| tek bir `DO` bloğu, uyku bloğun **dışında**, beş istisna alt-transaction'ı | — | **5 / 5 veri** | — |
| tek bir `DO` bloğu, uyku bloğun **İÇİNDE**, beş istisna alt-transaction'ı | **0 / 5** | — | **5 / 5 veri** |
| Sonrasında (`now()` satırı) | — | audit satırı **1**, tüketilmiş bilet **0** | — |

`statement_timestamp()`'in yalnız uyku ifadenin içindeyken yakalanması, §6'nın `DO`
testinin **uykuyu `DO`'nun içine** koymak zorunda olmasının sebebidir.

**Yazılan zaman damgaları (4. tur, O-4):** güvenlik denetimi ölçtü, bu turda yeniden
üretildi: 2 sn açık tutulan bir transaction'da `DEFAULT now()` bir satırı **2,007 sn**
geriye tarihledi, `DEFAULT clock_timestamp()` 0,001 sn (denetçinin 5 sn'lik ölçümünde
5,006 sn). Buna göre: `operator_audit_log`'un zaman sütunu `DEFAULT clock_timestamp()`'tir
ve INSERT listesinde **yoktur**; K6'nın tenant `audit_log` satırında `at` **açıkça**
`clock_timestamp()` ile yazılır (00005'in `DEFAULT now()`'ı kullanılmaz) — ve bu, `at`'ı
unutan bir `op_*`'ı `prosrc` ya da `pg_attrdef` taraması **yakalamadığı** için (DEFAULT
`audit_log`'da, `op_*`'ta değil) bir **davranış testiyle** pinlenir: uykusu tek bir `DO`
ifadesinin içinde olan bir tenant yazması, `at`'ın duvar saatine ≤1 sn yakın olduğunu
gösterir (OP-15/16 kabulü). Sonuç: bir
okumanın audit zamanı ile verinin döndüğü an arasındaki fark bilet ömrüyle, **≤ 60 sn**'yle
sınırlıdır.

Boşta-süre yüklemi aynı donmadan etkilenir (açık tutulan bir transaction'da oturum
"hep taze" görünür). Ve oturum düzeyi zaman aşımı buna çare **değildir**:
`idle_in_transaction_session_timeout`, `transaction_timeout`, `statement_timeout` üçü de
`context = user` (ölçüldü) — DSN sahibi kendi oturumunda 0 yapar. Bu yüzden kural
fonksiyonun **içindedir** — enrollment token süresi de artık bir definer'ın
(`op_complete_enrollment`) içindedir. Katalogda taranır (§6).

### 3. §4.5 sınırı — normatif

1. **Tenant bağlamı olmadan bir tenant satırına dokunan TEK kod yolu `op_*`'tır.**
   md.7'nin çözümleyicileri bu sayıma girmez (bağlamı üretirler, aşmazlar).
2. 🔴 **`op_*` gövdesinde RLS YOKTUR.** Sahibi `BYPASSRLS`'tir (ADR 0002'nin M0-03
   tablosu: `BYPASSRLS` rolü RLS'e tabi değildir). Ürünün her yerindeki kuşak+kemerin
   **kuşağı burada yoktur**; tek bir tenant'ı adlandıran her `op_*`'ın **her
   ifadesindeki** açık `tenant_id = p_tenant_id` filtresi **tek bariyerdir**. Bilerek
   bütün tenant'ları tarayan fonksiyonlar (tenant listesi) adıyla bellidir ve sabit
   sütun + LIMIT ile sınırlıdır.
3. **§1'in "asla" sütunları** — *"asla"* = `SELECT` yasağı — hiçbir `op_*`'ın
   `SELECT` yetkisinde ve dönüşünde yoktur. **İstisnalar, adıyla:**
   `platform_sessions.token_hash` (`WHERE`'de `SELECT`; döndürülmez) ve
   `op_complete_enrollment`'ın `platform_admins` digest/zarf **`UPDATE`**'i (yazma;
   `SELECT` yok). Yazma bu kuralın konusu değildir; her yazma yetkisi ayrı bir kararla
   adlandırılır (OP-18'in anahtar yazımı dahil).
4. **Veri, audit'i commit edilmeden dönmez** (§2 v): okumalar iki aşamalı, bilet ≤60 sn
   ve duvar saatiyle; yazmalar tek aşamalı ve yalnız `void`/`uuid` döndürür; tenant'ı
   değiştiren her yazma audit'ini hem operatör log'una hem o tenant'ın `audit_log`'una
   aynı transaction'da yazar (K6).
5. **Asla loglanmayanlar** (CLAUDE.md §7'nin listesine ek, bu ADR'nin kimlik bilgileri):
   **okuma bileti**, **TOTP kodu**, **enrollment token'ı** — hiçbir yerde: süreç log'u
   (`slog`), hata mesajı, audit `detail`'i, **ingress'in erişim log'u** (enrollment linki
   token'ı bu yüzden sorgu dizgisinde taşımaz — ADR 0020 §6). Ham hâlleri de hash'leri
   de.
6. **Go tarafında da tek yer.** `db.OperatorDB` ayrı bir tiptir; **`WithTenant` metodu
   yoktur**, `set_config('app.tenant_id', …)` çağırmaz (`resolve.go` emsali: bağlam
   kurmayan erişim); yalnız operatör handler'larına verilir; müşteri paneli operatör
   paketini **import etmez** (OP-7'de test).

### 4. Go tarafı

- **`db.OperatorDB`**, `TAPPA_OPERATOR_DATABASE_URL`'den açılır, kendi rol ölçümünü
  yapar ve üretimde ayrıcalıklı bir rolle açılmayı reddeder (`roleRefusal` kalıbı).
- **Bir okuma iki transaction'dır:** `op_begin_read` kendi transaction'ında commit
  edilir; `op_read_*` ondan **sonra** ayrı bir transaction'da koşar. İkisini tek
  transaction'da birleştiren bir erişimci yazılamaz — veritabanı zaten reddeder (§2 v),
  erişimci bu reddi bir hata olarak yüzeye çıkarır.
- **Oturumun hayatı definer'lardadır:** enrollment sonunda `op_complete_enrollment`,
  giriş sonunda `op_open_session`, her istekte `op_touch_session`, çıkışta
  `op_close_session`. `tappa_operator` `platform_admins`'e ve `platform_sessions`'a hiçbir
  şey yazmaz.
- DSN yoksa operatör yüzeyi **503**; müşteri paneli etkilenmez (ADR 0020 §4).
- ~~İki DSN'in yer değiştirmesi iki yönde de **gürültülü** başarısızlıktır:
  `tappa_app`'in `op_*` `EXECUTE`'u yok; `tappa_operator`'ın tenant tablolarında
  yetkisi yok.~~ → *(OP-7 notu, 2b, ölçülene daraltıldı:)* iki yön **eşit değil**.
  `TAPPA_OPERATOR_DATABASE_URL` `tappa_app`'e işaret ederse **açılış reddedilir** (operatör
  rol kapısı: `current_user ≠ tappa_operator`; `TestOperatorDB_RefusesEveryRoleButTappaOperator`).
  `DATABASE_URL` `tappa_operator`'a işaret ederse müşteri havuzunun kapısı **açılır**:
  ölçtüğü dört olgu (`rolsuper`, `rolbypassrls`, RLS'li tablo sahipliği, ayrıcalıklı bir
  role üyelik) `tappa_operator` için dördü de `false` (ölçüldü, `SET SESSION AUTHORIZATION`,
  geri alınan işlem) ve oturum değiştirilmemiştir; hata ancak bir tabloya ilk dokunuşta
  `42501` olarak görünür (ölçüldü: `tenants` üzerinde `SELECT` → *permission denied*).
  Sürecin o DSN'le açılıp ilk istekte düştüğü **kod çıkarımıdır** — `tappa_operator`
  geliştirmede NOLOGIN, süreç koşulmadı. Ucuz bir kod çaresi var ama **eklenmedi**
  (kapsam): üretimde müşteri havuzunun `current_user = tappa_operator` (ya da genel
  olarak `≠ tappa_app`) iken açılmayı reddetmesi — backlog adayı, OP-7 kart düzeltmesi
  "2b" satırında.
- **OP-7 notu (2026-10-01), ölçülerek** (ayrıntı: [m10-platform.md](../plan/m10-platform.md)
  → OP-7 kart düzeltmesi): (i) `db.OperatorDB` **kendisi bir `OperatorConn` değildir** —
  `Exec`/`QueryRow` yok; havuzu `OperatorConn`'dur ve yedi yöntemin her biri `operator.go`'nun
  aynı adlı fonksiyonuna devreder, yani SQL tek yerdedir ve ~~paket dışından operatörün
  bağlantısına kendi SQL'ini gönderen kod derlenmez~~ *(2i: `o.Exec`/`o.QueryRow` çağrısı
  derlenmez — ölçüldü: derleyici *"has no field or method Exec"* der (`operatorpool.go`'nun
  `OperatorDB` yorumu); yöntem kümesini ve devretmeyi `TestOperatorDB_EveryMethodDelegatesVerbatim`
  pinler *(2j: atıf önce yanlış teste gidiyordu)*; reflect ve `unsafe`
  ile havuza ulaşan kod üç parçalı iddianın PART III'üdür, kod incelemesinin konusu)*. (ii) Rol kapısı `current_user`'ın yanında
  **`session_user`**'ı da ister: sahibin DSN'ine `role=tappa_operator` başlangıç parametresi
  eklemek `current_user = tappa_operator`, `session_user = tappa_owner` verdi (ölçüldü) —
  bir `SET ROLE NONE` uzakta bir superuser oturumu (ölçüldü; `RESET ROLE` başlangıç
  parametresine döner). Kapı ayrıca **herhangi bir** role üyeliği
  reddeder (§1: "hiçbir rolün üyesi değil") ve **her ortamda** reddeder, yalnız üretimde
  değil. (iii) Açılışta operatör veritabanına **ulaşılamaması** açılışı durdurmaz
  (`db.ErrOperatorUnreachable` → yüzey 503); ulaşılan şeyin reddi durdurur. *(2. tur:
  "ulaşılamaz" KAPALI bir listedir — ağ, ad çözümü, zaman aşımı, iptal edilmiş açılış,
  SQLSTATE sınıfı 08 ve 28, `3D000`, `53300`, `57P01`–`57P03`; ilk hâl ping'deki HER
  SQLSTATE'i ulaşılamaz sayıyordu ve denetçi `42501`, `22023`, `42704` ile açılışı sürdürdü.
  Kapı ayrıca `rolcreatedb`, `rolcreaterole`, `rolreplication`'ı ve rolün bir ÜYESİ
  olmasını da reddeder. Aynı `session_user` ölçütü **müşteri havuzunun** kapısına da
  eklendi — sahibin DSN'i + `role=tappa_app` üretimde açılıyordu; artık üretimde ret,
  geliştirmede uyarı.)*

### 5. Rollerin kurulumu

- **`scripts/db-init/01-roles.sql`:** iki rol (`tappa_operator` NOLOGIN parolasız;
  `tappa_opdefiner` NOLOGIN BYPASSRLS) + `CONNECT`/`USAGE`. **Migration'da değil**,
  çünkü: roller küme düzeyindedir; ve `redline-check.sh` R5b migration'daki bir
  `BYPASSRLS` rolünü FAIL eder — kapsamı bilerek `db/migrations/*.sql`'dir ve
  `01-roles.sql` `tappa_resolver` yüzünden tarama dışıdır (script'in kendi yorumu).
- **`01-roles.sql` yalnız boş `PGDATA`'da koşar** (dosyanın kendi başlığı). Çalışan
  geliştirme veritabanı ve canlı küme için **tek seferlik bir runbook** gerekir; canlı
  kümede bu **kullanıcı işidir** — ajan `tappa-secrets`'a dokunmaz (T44/T45 emsali).
  Geliştirme parolası `tappa_app`'in dev-only script emsaliyle, ~~üretim parolası
  Secret'tan (`deploy/k8s/postgres-init/02-app-password.sh` emsali).~~ → *(OP-7 notu,
  2026-10-01: üretim parolası psql `\password` ile stdin'den verilir; ayrı bir Secret
  anahtarı yoktur, yalnız DSN'in içindedir — runbook 2. adım.)*
- 🔴 **Sır değerleri hiçbir dosyaya yazılmaz** (Olay A-0). Runbook ve manifestler
  yalnız adlarla konuşur. Kullanıcının `tappa-secrets`'a ekleyeceği adlar (ADR 0020
  Sonuçlar'daki listeyle **aynı**): `TAPPA_OPERATOR_DATABASE_URL`,
  `TAPPA_OPERATOR_TOTP_KEK`, `TAPPA_OPERATOR_TOKEN_HMAC_KEY` ve `tappa_operator`
  rolünün parolası.
  `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest` yeni sır adlarının
  manifestte enjekte edildiğini zorlar. → **OP-7 notu (2026-10-01):** dört ad, parola
  aralarında değil — ADR 0020 Sonuçlar'daki aynı tarihli not; dördünün `optional: true`
  olarak Secret'tan geldiğini `TestPackaging_TheOperatorSurfaceIsOneOptionalSecretSet`
  zorlar. Geliştirme parolası `scripts/db-init/02-dev-only-password.sh`'te.

### 6. Denetim şekli — neyle tutulur

R5b bir `SECURITY DEFINER` fonksiyonunu kötü niyetlisinden **ayırt edemez**
(`redline-check.sh`, *"KAPATILAMAYANLAR"*: *"urunun KENDI cozucusu tam olarak budur"*).
Bu sınırı tutan şey katalog ve davranış testleridir (adları görev kartlarında konur;
hangi testin hangi karta düştüğü plan bloğunda):

- **Katalog — ileri yön:** `tappa_opdefiner`'ın sahip olduğu her fonksiyonun adı `op_`
  ile başlar, `SECURITY DEFINER`'dır, `proconfig`'i tam olarak
  `search_path=pg_catalog, pg_temp`'tir, PUBLIC ve `tappa_app` için `EXECUTE` `false`,
  `tappa_operator` için `true`'dur. İlk argümanı oturum hash'idir — **üç adlı istisna:**
  `op_record_auth_event`, `op_open_session`, `op_complete_enrollment`. Adı `op_read_` ile başlayan her fonksiyon
  ikinci argüman olarak (ham) bileti alır. **Dönüş tipi pini:** `op_read_%`,
  `op_begin_read` ve `op_touch_session` dışındaki her `op_*` için `proretset = f` ve
  dönüş tipi `void` ya da `uuid` — yazma kuralının (§2 v 7) katalogdan okunabilen yarısı.
  Okunamayan yarısı (bir `void` fonksiyonun **hata** ayrımıyla durum sızdırmaması) kod
  incelemesiyle ve davranış testiyle tutulur.
- **Katalog — donan saat taraması (4. tur, O-1):** `tappa_opdefiner`'ın sahip olduğu
  fonksiyonların `prosrc`'unda (büyük/küçük harf duyarsız) §2 vii'nin yasak adlarından
  **hiçbiri** geçmez; ve bu ADR'nin tablolarındaki zaman DEFAULT'ları (`pg_attrdef`)
  `clock_timestamp()`'tir. (Tarama bir ad listesidir; bir takma adın ya da bir yardımcı
  fonksiyonun arkasındaki donan saati görmez — o, kod incelemesinin ve süre davranış
  testlerinin işidir.)
- **Katalog — TERS yön** (ileri yön, sahipliği unutulmuş bir fonksiyonu **göremez**:
  `ALTER FUNCTION … OWNER TO tappa_opdefiner` satırı unutulan bir `op_*` superuser
  sahipli bir definer olur — tam bypass — ve yukarıdaki her kontrol yeşil kalır):
  - `public`'teki adı `op\_%` olan **her** fonksiyonun sahibi `tappa_opdefiner`;
  - `prosecdef` olan **her** fonksiyonun sahibi {`tappa_resolver`, `tappa_opdefiner`}
    içinde ve o sahip `rolsuper = f` (bugün: altı fonksiyon, altısı `tappa_resolver`,
    `f` — ölçüldü, yani test ilk gün yeşil doğar);
  - `pg_auth_members`'ta `tappa_opdefiner`'ın üye sayısı **0**.
- **Katalog — roller ve tablolar:**
  - `has_column_privilege('tappa_operator', 'platform_sessions', 'token_hash',
    'SELECT') = false`; `tappa_operator` `platform_sessions`'ta dört fiilde yetkisiz;
  - `tappa_operator` **bilet tablosunda dört fiilde yetkisiz**;
  - `tappa_operator` hiçbir tenant tablosunda `has_any_column_privilege` = `true` değil;
    `operator_audit_log`'a `INSERT` tutmaz;
  - `has_any_column_privilege('tappa_operator', 'platform_admins', 'UPDATE') = false`,
    aynısı `'INSERT'` için; `has_table_privilege('tappa_operator', 'platform_admins',
    'DELETE') = false`; enrollment token hash'inde `has_column_privilege(…, 'SELECT') =
    false`;
  - `tappa_opdefiner`: `platform_admins` üzerinde `INSERT` yok; `platform_admins`'in
    parola digest'i ve TOTP zarfında `has_column_privilege(…, 'SELECT') = false` (yazar,
    okumaz); bilet tablosunda `UPDATE` yalnız `consumed_at`'te (`created_xact` için
    `has_column_privilege(…, 'UPDATE') = false`); **bilet tablosunda
    `has_column_privilege(…, 'created_xact' | 'created_at' | 'consumed_at', 'INSERT') =
    false`** (O-2); **`operator_audit_log`'un zaman sütununda `INSERT` = `false`** (O-4);
    §1'in "asla" sütunlarında `has_column_privilege(…, 'SELECT') = false` — yetki türü
    açıkça `'SELECT'`;
  - `tappa_operator` `operator_audit_log` üzerinde `has_any_column_privilege(…, 'SELECT')
    = false`;
  - `tappa_app` bu ADR'nin her tablosunda dört fiilde yetkisiz — üretimin geniş default
    ACL'i simüle edilerek (OP-5 kabulü) — ve varsa dizilerinde de.
- **Davranış — oturum:** süresi dolmuş (mutlak ve boşta) / MFA'sız / iptal edilmiş
  oturumun ya da `disabled` operatörün hash'i → exception ve 0 audit satırı.
- **Davranış — iki aşamalı okuma** (§2 v tablosunun kalıcı hâli): aynı transaction'da
  yaratılan bilet ret; bir **savepoint** içinde yaratılan bilet ret; commit edilmemiş
  bilet eşzamanlı başka bir oturumdan görünmez; commit sonrası kullanım veri döndürür;
  okuma transaction'ı geri alınsa da audit satırı kalıcıdır; kabul edilen her okuma tam
  1 audit satırı (`op_begin_read`'de); tüketilip commit edilmiş bilet ikinci kez ret;
  farklı bir parametreyle (sayfa/imleç/terim) kullanılan bilet ret.
- **Davranış — bilet süresi** (§2 vii): **süresi dolmuş bilet → ret**; ve **açık
  tutulan bir transaction içinde süre dolunca → ret** — hem savepoint geri alma ile hem
  bir `DO` bloğunun istisna alt-transaction'ıyla, **uyku `DO`'nun İÇİNDE**. `now()` ve
  `statement_timestamp()` mutasyonlarının her biri en az bir testi kırmızıya çevirmeli.
- **Davranış — bilet sahteciliği:** `created_xact` ya da `created_at` açıkça yazılmaya
  çalışılan bir bilet `INSERT`'i `42501`; `created_xact` `'1'` ya da `'2'` olan satır
  CHECK ile reddedilir.
- **Davranış — yazmanın dönüşü:** tenant durumunu değiştiren bir yazmanın dönüşü ve
  hata davranışı hedefin önceki durumundan bağımsızdır (aynı çağrı "değişti", "zaten
  öyleydi" ve **"hedef tenant yok"** durumlarında aynı sonucu verir; yoksa tenant
  `audit_log` satırı yazılmaz — B14).
- **Davranış — oturum öncesi yazma kapısı:** limiter'ın reddettiği giriş isteği
  `operator_audit_log`'a satır yazmaz ve sayacı artırmaz; kilit kararı audit satırlarını
  saymaz, hesabın sayacını okur (sayacı artıran her çağrı bir `totp_failed` satırı
  bırakır); `op_open_session` sayacı sıfırlar.
- **Davranış — TOTP adımı ve oturum doğuşu:** aynı adımla ikinci `op_open_session` →
  EXCEPTION, oturum yok, köken satırı yok; `pending` ya da `disabled` hesap için →
  EXCEPTION; **zehirli adım** (`bigint` üst sınırı ya da `cur ± 2`) → EXCEPTION ve hesap
  kilitlenmez (ardından `cur` kabul edilir); **`cur - 1`, `cur`, `cur + 1`** kabul edilir;
  kilit koşulu sağlanmamış hesap → EXCEPTION (kilit aynı `UPDATE`'te).
- **Davranış — enrollment:** süresi duvar saatiyle dolmuş token ret; kullanılmış token
  ret; id ile token hash'inin eşleşmediği çağrı ret; `active` hesaba ikinci enrollment
  ret; başarıda `totp_last_step` verilen adımdır ve **aynı adımla** hemen ardından
  gelen `op_open_session` reddedilir (enrollment kodu girişte tekrar oynatılamaz); üç
  ret de aynı hatayı verir; zehirli ilk adım reddedilir; **aynı token'la N eşzamanlı
  çağrı → tam 1 başarı** (emsal `internal/db/invites_test.go:213`).
- **Davranış — kemer:** A tenant'ı için çağrılan tenant-tekil bir `op_*` B'nin satırını
  ne döndürür ne değiştirir (RLS'siz koştuğu için tek kanıt odur).
- **Davranış — geçici tablo gölgesi:** çağıran oturumda aynı adlı geçici tablo açılır
  ve 🔴 **çağıran onu `tappa_opdefiner`'a `GRANT` eder** — bu adım ŞARTTIR: onsuz,
  `search_path` bozulmuş bir fonksiyon da `permission denied` ile düşer ve test onu
  "reddedildi" diye yeşil sayar (iki denetçi ayrı ayrı ölçtü; 2. turda burada yeniden
  ölçüldü, §2 iv tablosu). Fonksiyon yine gerçek tabloyu okumalı.

## Elenen seçenekler

| Seçenek | Neden elendi |
|---|---|
| **Operatöre `BYPASSRLS` bir LOGIN havuzu** | `pool.go` `roleRefusal` onu üretimde **açılışta reddeder** (ölçülmüş kod yolu). Ve reddetmeseydi: o havuzdaki **her** sorgunun kemeri tek bariyer olurdu — tek eksik `WHERE` her tenant'ı açar. `op_*`'ta aynı risk yalnız adı konmuş, sabit sütunlu, LIMIT'li fonksiyonlarda vardır |
| **Operatörün her tenant için `WithTenant` döngüsü** | Tenant listesi **çıkarılamaz** (`tenants` politikası bağlamdaki tek satırı gösterir — liste için önce listeyi bilmek gerekir); ve ADR 0016 §2'nin yasakladığı deseni (`WithTenant(başkası)`) **normalleştirir** |
| **`op_*`'ların sahibini `tappa_resolver` yapmak** | Patlama yarıçaplarını birleştirir: `tappa_resolver`'ın yetkisi `tappa_app`'in çağırdığı çözümleyicilerin yetkisidir (ADR 0002 md.7: *"patlama yarıçapı o role verilen GRANT'larla bu tablolara sınırlanır"*). Operatörün geniş sütun yetkileri oraya eklenseydi, bir çözümleyici kusuru operatörün yarıçapını kazanırdı |
| **`op_*` `EXECUTE`'unu `tappa_app`'e de vermek** | Müşteri uygulamasının rolüyle koşan her SQL kusuru (enjeksiyon, hata) operatör yüzeyine ulaşırdı. Ayrı LOGIN rolü, müşteri tarafı bir SQL kusurunu `op_*`'tan **yapısal olarak** ayırır (aynı süreçteki RCE'yi ayırmaz — sınır 2) |
| **`op_*` erişimcilerini sqlc ile üretmek** | Teknik: sqlc v1.28 `RETURNS TABLE(...)` çağrısını tipleyemiyor (ADR 0002 md.7, üç kez ölçüldü) |
| **`SET search_path = pg_catalog, public`** (plan metni) | Ölçüldü, açık — §2 (iv) |
| **Tek aşamalı okuma** (audit ve veri aynı transaction'da) | Güvenlik denetimi ölçtü: `SAVEPOINT` → çağrı → `ROLLBACK TO` veriyi verir, audit'i siler — §2 (v) |
| **Bileti `xmin = üst düzey xid` ile reddetmek** | Ölçüldü: savepoint içinde yaratılan bilet geçer (A2) — §2 (v) 3 |
| **`pg_xact_status` koşulunu `IF … <> 'committed'` ile yazmak** | Ölçüldü: eski xid'de `NULL`; `NULL <> 'committed'` `NULL` → dal atlanır, fail-open — §2 (v) 4 |
| **Süreyi `now()` ile karşılaştırmak** (deponun deyimi) | Ölçüldü: açık tutulan transaction'da süresi dolmuş bilet 3/3 ve 5/5 veri verdi — §2 (vii) |
| **Oturum düzeyi zaman aşımlarına güvenmek** | Üçü de `context = user` (ölçüldü); DSN sahibi 0 yapar — §2 (vii) |
| **`op_read_*`'ın bilet HASH'ini alması** | Saklanan değer kimlik bilgisi olurdu; `token_hash`'in dersi — §2 (v) 2 |
| **`tappa_operator`'a bilet tablosunda `INSERT`/`UPDATE`** | Güvenlik denetimi ölçtü: audit'siz bilet basıldı, tüketim sıfırlandı — §1 |
| **Yazmanın sayı ya da durum döndürmesi** | Güvenlik denetimi ölçtü: `SAVEPOINT` + yazma → sayı + `ROLLBACK TO` = izsiz durum kehaneti — §2 (v) 7 |
| **`tappa_operator`'a `operator_audit_log` üzerinde doğrudan `INSERT`** | DSN'i tutan her türlü satırı — başarı dahil — basar; audit bir kanıt olmaktan çıkar |
| **Oturumsuz, genel bir audit definer'ı** | (i)'yi deler; yerine kapalı türlü üç dar istisna (§1) |
| **`statement_timestamp()` ya da başka bir donan saat** | Ölçüldü: uyku tek bir `DO` ifadesinin içindeyken süresi dolmuş bilet 5/5 veri verdi — §2 (vii) |
| **Bilet satırında `created_xact`/`created_at`'i yazılabilir bırakmak** | Güvenlik denetimi ölçtü: `created_xact = '2'` (`committed` döner) + `created_at = +1 yıl` aynı transaction'da veri verdi — §2 (v) 1, 4 |
| **TOTP adımını yalnız `totp_last_step < $adım` ile sınırlamak** | Güvenlik denetimi ölçtü: `bigint` üst sınırı kabul edildi ve hesap kalıcı kilitlendi — §1, `op_open_session` |
| **Audit zamanını `DEFAULT now()` ile yazmak** | Ölçüldü: 2 sn açık transaction'da 2,007 sn geriye tarihli — §2 (vii) |
| **Arama teriminin hash'ini audit'e ya da bilet satırına yazmak** | Anahtarsız hash sözlükle geri çevrilir (ADR'nin kendi e-posta gerekçesi); terim bilet hash'inin içinde bağlanır — §2 (v) 1 |
| **`tappa_operator`'a `operator_audit_log` üzerinde `SELECT`** | Operatör audit'i tenant-ötesi bir okuma kaydıdır; okunması da iki aşamalı ve audit'li olmalı (`op_read_audit`) — §1 |
| **TOTP kilidini `totp_failed` audit satırlarından türetmek** | Sahte satır basan gerçek bir kilit DoS'u üretirdi — §1 |
| **Touch ifadesini `tappa_operator`'a `SELECT (token_hash)` vererek yazmak** | Güvenlik denetimi ölçtü: rol bütün canlı hash'leri listeledi — §1 |
| **Oturumu `tappa_operator`'ın doğrudan `INSERT`'iyle yaratmak** | Köken satırı olmayan oturumlar mümkün olurdu; `op_open_session` aynı gücü iz ile verir — §1 |
| **Enrollment tüketimini `tappa_operator`'ın ifadesi olarak bırakmak** | O rolün `platform_admins` kimlik bilgisi `UPDATE`'i DSN sahibine izsiz enrollment ve kimlik bilgisi yeniden yazımı veriyordu (eski sınır 10) — §1, `op_complete_enrollment` |
| **TOTP adım ilerletmesi için ayrı bir definer** | Dördüncü oturumsuz ad olurdu ve tekrar korumasıyla oturumun doğuşunu iki ifadeye bölerdi — §1, `op_open_session` |
| **Enrollment hesabını bulan bir arama definer'ı** (id'yi linke koymak yerine) | Dördüncü oturumsuz ad olurdu; id bir sır değil — §1 |

## Sayılı sınırlar

1. **(i) uygulama kodunu durdurur, `tappa_operator` DSN'inin sahibini oturum basmaktan
   durdurmaz.** Giriş akışı süreçte doğrulanır (TOTP KEK süreçte); `op_open_session` bu
   doğrulamayı yapamaz, çağırana güvenir. DSN'i tutan biri `op_open_session` ile MFA'lı
   bir oturum basıp onun hash'iyle `op_*` çağırabilir. Onu izleyen şey audit'tir: basılan
   **her oturumun** commit edilmiş bir köken satırı vardır (§1) ve o oturumla veri
   döndüren her çağrı **commit edilmiş** bir audit satırı bırakır (§2 v); satırlar oturum
   kimliğini taşır (§2 v, *"zorunlu içerik"*). ADR 0020 risk 3–4.
2. **Aynı süreçte RCE her iki DSN'i de ele geçirir (K9).** Ayrı Deployment (OP-19)
   bunu kapatırdı.
3. **Başarısız oturum sondaları DSN sahibince gizlenebilir.** `op_begin_read`'in ya da
   bir yazmanın reddi çağıranın kendi transaction'ında olur ve geri alınabilir (§2 v 9,
   ölçüldü). Geçerli bir oturum hash'ini tahmin etmek 256 bitlik bir uzayda aramaktır
   (oturum token'ı 256 bit — ADR 0020 §2), pratikte imkânsız; yani gizlenebilen şey
   **başarısız** denemelerdir, başarılı bir okuma değil.
4. **Bilet, geri alınan bir okumadan sonra en çok 60 saniye içinde yeniden
   kullanılabilir.** Ölçüldü (B1): okuma transaction'ı `ROLLBACK` edilince bilet
   tüketilmemiş hâle döner ve ikinci kullanım veri verir; commit edilmiş tüketimden sonra
   üçüncü kullanım ret. Pencere **≤ 60 sn**'dir çünkü ömür şemada tavanlıdır ve duvar
   saatiyle karşılaştırılır (§2 v 1, vii — `now()` ile pencere, transaction açık
   tutuldukça sınırsızdı). Yeniden kullanım **yeni bir audit satırı bırakmaz**; ama zaten
   commit edilmiş satır aynı oturumu, aynı türü, aynı hedef kapsamını ve aynı
   sayfayı/imleci adlandırır, ve bilet hash'i aynı terimi bağlar — yani yeniden okunan şey
   tam olarak audit'te kaydı olan okumadır (terimin kendisi audit'te yazılı değildir).
5. **Audit append-only'dir, mutlak değildir.** Satır ve tablo-boşaltma trigger'ları
   `tappa_owner` için de hata verir; ama bir superuser trigger'ı `DISABLE` edebilir ya
   da tabloyu `DROP` edebilir — 00005'in kendi ifadesiyle *"defense-in-depth, mutlak
   degil"* (00005:22-23), 00021'in ifadesiyle *"defence in depth … not an absolute"* ve
   *"it does not stop DROP TABLE"* (00021:79-82).
6. **Kemer tek bariyerdir** (§3.2). Bir `op_*`'ta unutulan tek `tenant_id` filtresi o
   fonksiyonun sabit sütunlarını, LIMIT'i kadar, bütün tenant'lara açar. Fren: kod
   incelemesi + kemer davranış testi.
7. **`op_record_auth_event` sahte başarısızlık satırlarına açıktır** (§1): DSN sahibi
   o fonksiyon üzerinden gürültü basabilir, ~~**o fonksiyon üzerinden** sahte bir başarı
   basamaz~~ *(OP-14 düzeltmesi, 2026-10-06: 00031'den beri **sahte bir `password_ok`
   basabilir** — herhangi bir hesap id'si için *"parolası biliniyor"* diyen yanlış bir
   alarm; satır oturum açmaz, aktör iddia etmez, kilit sayacını ne artırır ne sıfırlar.
   Sahte bir **oturum doğuşu** ise bu fonksiyondan basılamaz:)* (başarı satırları `op_open_session`'ın köken satırlarıdır ve onlar sınır 1'e
   tabidir: gerçek bir oturumla birlikte doğarlar). Go tarafındaki süreç geneli tavanı
   DSN sahibi atlar. ⚠️ **Ve DSN sahibi bir hesabı kilitleyebilir:** sayacı artıran yol
   (`totp_failed`) çağırabildiği bir definer'dır; **sayaç için** bunu durduracak
   veritabanı tarafı bir yol yoktur, çünkü sayaca giden **her** yol `tappa_operator`'ın
   çağırabildiği bir definer olmak zorundadır. Her artış bir audit satırı bırakır; çare
   `opadmin` (`tappa_owner`). **TOTP adımı için ise veritabanı tarafı bir yol VARDIR ve
   kullanılır:** adım duvar saatine bağlıdır (`cur ± 1`, §1), dolayısıyla DSN sahibi
   adımı ileri zehirleyip bir hesabı **kalıcı** kilitleyemez (4. tur, güvenlik denetimi).
8. **Elle tiplenmiş erişimciler SQL'den sapabilir** (m10-platform.md §7). Testler sevk
   edilen SQL'i koşturur; `resolve.go`'nun aynı sınırı ve aynı çaresi.
9. **Kişisel veri "asla" listesinde değildir.** `transactions.gps_lat`/`gps_lng`,
   `transactions.source_ip`, `employees.email`, `admin_users.email` sır değil kişisel
   veridir; `op_*`'ların onları okuyup okumayacağı A2 kartlarının (OP-11…OP-13)
   veri-minimizasyon kararıdır. Okunurlarsa CLAUDE.md §7'nin *"asla loglanmaz"*
   listesi (tam GPS koordinatı dahil) onlar için de geçerlidir: audit `detail`'ine ve
   süreç log'una girmezler (ADR 0020 §5).
10. ~~Enrollment tüketimi `tappa_operator`'ın ifadesi kaldıkça DSN sahibi bekleyen bir
    hesabın enrollment'ını tamamlayabilir ya da bir hesabın parola/TOTP'sini izsiz
    yeniden yazabilir.~~ **KAPANDI (3. tur eki): enrollment ve kimlik bilgisi yazımı
    definer'da** — `op_complete_enrollment` ham token ister ve köken satırını aynı
    ifadede yazar; `tappa_operator`'ın `platform_admins`'te hiçbir yazma yetkisi yoktur
    (§1). (Numara, atıflar kaymasın diye korunur.)
11. **Giriş araması `tappa_operator`'ın `SELECT`'i kaldıkça** (öneri: §1 sonu), DSN
    sahibi **bütün operatörlerin parola digest'lerini** okuyup çevrimdışı kırmayı
    deneyebilir — bcrypt cost 12 ve en az 14 rune (ADR 0020 §1) bunu pahalı kılar,
    imkânsız değil. **Mühürlü TOTP sırlarını da okur** ama `TAPPA_OPERATOR_TOTP_KEK`
    olmadan açamaz; aynı süreçteki bir RCE KEK'i de alır (sınır 2, K9).
12. **Kimliksiz enrollment son adımı pahalıdır.** `POST /operator/enroll` token
    veritabanında doğrulanmadan önce bir cost-12 bcrypt ve bir `Seal` öder
    (`tappa_operator` token'ı önceden kontrol edemez). Çare, ADR 0015'in aynı sınıftaki
    emsaliyle, adres başına ve süreç geneli bir **oran sınırıdır** (sayılar OP-8).
13. **TOTP adımı iki saate bağlıdır.** Go kodu kendi saatine göre doğrular, veritabanı
    adımı kendi saatine göre `cur ± 1` ile sınırlar; iki saat bir adımdan (≈30 sn) fazla
    ayrışırsa geçerli kodlar reddedilir. Yön fail-closed'dır; çare saat eşitlemesidir
    (NTP), kod değil.
14. **`op_open_session` bir SAVEPOINT kehanetidir (OP-5 2. tur, 2026-09-26).** DSN sahibi
    çağrıyı bir `SAVEPOINT` içinde yapıp geri alarak, iz bırakmadan, bir hesabın bu ve
    komşu adımlarda kod kullanıp kullanmadığını öğrenir (`totp_last_step`,
    `tappa_operator`'ın SELECT edemediği sütun). *(4. tur düzeltmesi: bu madde kilit
    durumunu da "SELECT edilemez" sayıyordu; `totp_locked_until` giriş aramasının sütun
    listesindedir ve operatör onu doğrudan okur — ölçüldü. Kehanetin yalnız adım yarısı
    yeni bilgidir.)*
    Ölçüldü (dev, `BEGIN … ROLLBACK`, `tappa_operator` olarak; hesabın `totp_last_step`'i
    `cur`): doğrudan `SELECT totp_last_step` → *permission denied*; `SAVEPOINT` içinde
    `op_open_session(…, cur)` → ret, `(…, cur+1)` → kabul; iki `ROLLBACK TO` sonrası audit
    farkı 0, oturum 0, `totp_last_step` değişmemiş. Etkisi sınır 1'in içindedir (aynı kişi
    o hesaba MFA'lı oturum basabilir); yazıldı çünkü §2 v 7'nin kapsam notu bunun tersini
    söylüyordu.
    **İkinci kanal, aynı bilgi (3. tur, 2026-09-26):** `op_open_session` ve
    `op_complete_enrollment`'ın kısıt yakalayıcısının LOG satırı yalnız sunucuya gitmez —
    `client_min_messages` bir **kullanıcı** ayarıdır ve `SET LOCAL client_min_messages =
    log` diyen çağırana da döner; ve satır yalnız bütün koşullar tuttuğunda doğar. Ölçüldü
    (dev, `BEGIN … ROLLBACK`, `tappa_operator`): koşullar tutarken bozuk hash → çağırana
    `LOG: … failed constraint "platform_sessions_token_hash_check" (SQLSTATE 23514)`;
    aynı hash, bayat adım → LOG yok. Değer taşımaz (kısıt adı + SQLSTATE), ama "koşullar
    tuttu" bitini SAVEPOINT'siz de verir. Ölçülüp **alınmayan** kapatma: fonksiyona
    `SET client_min_messages = error` iğnelemek satırı çağırandan keser (sunucu yine
    yazar — ölçüldü), ama iki eşdeğer kanaldan yalnız birini kapatır, DSN sahibinin
    öğrenebileceğini azaltmaz ve §6'nın normatif tek-girdili `proconfig` pinini değiştirir.
15. **İstatistik görünümleri bir varlık kehanetidir (OP-5 4. tur, 2026-09-26).**
    `pg_stat_xact_user_tables` ve `pg_stat_user_tables` her role açıktır ve SAVEPOINT geri
    almalarından etkilenmez. Ölçüldü (dev, `BEGIN … ROLLBACK`, `tappa_operator`, her
    çağrı ayrı bir `SAVEPOINT` içinde ve `ROLLBACK TO` ile): `platform_admins` üzerinde
    `op_record_auth_event('login_failed', <adres>)` farkı — bilinmeyen adres
    `seq_scan +1` (iki ayrı adreste aynı), **`pending`** hesabın adresi **`+2`**,
    **`disabled`** hesabın adresi **`+2`** (`seq_tup_read` da farklı). Fazlalık, hedef
    dolunca koşan `target_admin_id` yabancı anahtar kontrolüdür. Yani RLS'in giriş
    aramasından gizlediği `pending`/`disabled` hesapların **adresleri** iz bırakmadan
    sınanabilir; `n_live_tup` ise gizliler dahil **toplam** hesap sayısını verir.
    Definer tarafında kapatılamaz: görünümler herkese açıktır ve sayaçlar plana bağlıdır
    (tablo büyüyüp plan indekse dönünce ayrım `idx_tup_fetch`'e taşınır, yani sayacı
    "eşitleyen" bir yama bir sonraki planda bozulur). Ölçülen ama **uygulanmayan**
    daraltma: adres yolunu yalnız `active` hesaplara çözmek bugünkü `seq_scan` farkını
    kapatır, ama 2. turun D3 kararını (gizli hesabın adresi satırda hedef olarak kalır)
    geri alır ve plan değişince aynı sınıf başka sayaçta geri gelir. Etkisi sınır 1'in
    içindedir; e-posta tahmini gerektirir ve `id` yolu 122 bitlik rastgele değerle
    sınanamaz.

## Karar verilmedi

- `tappa_operator`'ın `platform_admins` üzerindeki **kesin** sütun listesi ve her
  `op_*`'ın kesin sütun grant'ları — OP-5 / ilgili A2 kartı. → **OP-5'te karara bağlandı
  (2026-09-26):** `tappa_operator` yalnız `SELECT (id, email, display_name, status,
  password_hash, totp_secret_sealed, totp_locked_until)`, ve yalnız `status = 'active'`
  satırlar (tek RLS politikası) — sınır 11 aktif hesaplara daralır. `tappa_opdefiner`'ın
  listeleri `db/migrations/00026_create_platform_operator.sql`'de, tam liste olarak
  `internal/db/operatorschema_test.go`'da pinli. Aşağıdaki "OP-5 uygulama notu".
- **Giriş aramasını definer'a taşımak** (öneri; enrollment kısmı 3. tur ekinde karara
  bağlandı). `tappa_operator`'ın parola digest'i ve TOTP zarfı üzerindeki `SELECT`'ini
  kaldırır (sınır 11). Benimsenirse güncellenen kurallar §1 sonunda sayılıdır
  (`tappa_opdefiner`'ın *"digest/zarf okumaz"* kuralı ve ayrı rol, `prosecdef` sahip
  kümesi, dördüncü oturumsuz ad) — OP-5/OP-6. → **OP-6 (2026-09-26): BENİMSENMEDİ** —
  yeni bir definer ve rol bir migration ister, OP-6'da migration yoktur; sınır 11 aynen
  geçerli, madde açık.
- Enrollment'ta sırrın **gösterildiği** adım ile ilk kodun **doğrulandığı** adım arasında
  düz TOTP sırrının nerede tutulduğu (süreç belleği mi, şifreli ve imzalı kısa ömürlü bir
  ara çerez mi; DB'ye ve log'a asla) — OP-6/OP-8. ⚠️ **"Süreç belleği" seçeneğinin
  riski:** kimliksiz bir `GET /operator/enroll` ile anahtarlanan bir bellek kaydı,
  sınırsız sayıda istekle doldurulabilen bir **bellek ayırma ilkeline** dönüşür; bu
  seçenek seçilirse kayıt sayısı ve ömrü tavanlı olmak zorundadır. → **OP-6'da karara
  bağlandı (2026-09-26): SUNUCUDA HİÇBİR YERDE.** Sır, sayfanın gizli form alanında taşınan
  bir **bekleyen blob**a `sun.Seal` ile mühürlenir (AAD = etiket ‖ hesap id ‖ son geçerlilik,
  30 dk); bellek seçilmediği için kayıt sayısı ve ömrü sorusu doğmaz. Blob'un AAD'si saklanan
  zarfınkinden (yalnız id) farklıdır, yani biri ötekinin yerine iki yönde de geçemez; sır
  DB'ye ancak ilk kod doğrulandıktan sonra saklanan AAD ile yeniden mühürlenerek gider
  (`internal/operatorauth/enrollment.go`).
- **Reddedilen `op_*` çağrısının** Go tarafından ayrı bir transaction'da
  `operator_audit_log`'a yazılıp yazılmayacağı — **OP-5** (reddedilebilen ilk çağrı
  OP-8'in oturum kapısıdır, `op_touch_session` ise OP-5'te doğar). Yazılırsa bir tür
  gerekir ve `op_record_auth_event`'in kapalı kümesi bu ADR'nin bir güncellemesiyle
  büyür. Her durumda sınır 3 geçerlidir: o iz dürüst bir süreç hatasına karşı
  kanıttır, DSN sahibine karşı değil. → **OP-5'te karara bağlandı (2026-09-26): YAZILMAZ,
  küme büyümez.** Oturum kapısının girdisi internetten gelen bir çerezdir; ret başına bir
  satır append-only bir tabloya kimliksiz, sınırsız bir yazma ilkeli olurdu
  (`adminlogin.go:1300-1301`'in dersi) ve bağlayacağı bir kimlik yoktur. Saldırıya değen
  retlerin kendi türleri var; kapının reddi süreç log'una hash'siz yazılır (OP-8).
- **Oturum öncesi satırların süreç geneli tavanının sayısı** — OP-6/OP-8; ve
  `op_record_auth_event`'in **içinde** ikinci bir tavan (zaman penceresinde satır
  sayısı; DSN sahibini de bağlar) — OP-5. → **OP-5'te ölçüldü ve KONMADI (2026-09-26):**
  doğal biçimi (10 dk'da ≤ N satır, satır ve sayaç birlikte) rolled-back bir sondada, N=20:
  25 bilinmeyen-adres çağrısı 20 yazdı / 5 kırpıldı; ardından kurbana 5 yanlış TOTP **0
  yazdı**, sayaç **0** kaldı — parola gerektirmeyen bir sel TOTP kilidini kapatıyor.
  `totp_failed`'ı muaf tutmak ise DSN sahibinin `totp_failed` yazımını sınırsız bırakır;
  tavan varlık sebebini kaybeder. Sınır 7 olarak sayılı kalır. → **Go tarafı sayısı OP-6'da
  (2026-09-26): 30 satır / 10 dk, tek anahtar, yalnız parolasız türler** (`totp_failed` ve
  `locked` neden dışarıda: §1 (a)'nın altındaki OP-6 düzeltmesi). Satır bedeli (2026-09-30'da
  iki bağımsız ölçüm; ilk okuma olan 139,3 bayt yeniden üretilemedi): tablonun üç indeksli
  geçici kopyasında hedefsiz satır 158,6–170,4, hedefli satır 174,7–188,4 bayt — bir gözlem
  aralığı, koşudan koşuya değişir → en kötü sürekli durum ≈ 250–297 MB/yıl. Tavan aşılınca istek
  yine hizmet görür, yalnız satır yazılmaz — bir iz susturma ilkeli, pencere başına tek WARN.
- **Commit'ten bağımsız ikinci iz** (savunma derinliği): örn.
  `ALTER ROLE tappa_operator SET log_statement = 'all'` **ve**
  `log_parameter_max_length = 0` — ikincisi şarttır, yoksa oturum hash'i ve bilet
  parametre olarak sunucu log'una yazılır (CLAUDE.md §7; bu ADR §3.5). İkisi de yalnız
  superuser'ın değiştirebildiği ayarlardır (ölçüldü: `context = superuser`), yani DSN
  sahibi kendi oturumunda kapatamaz. Alternatif: pgaudit (yeni bağımlılık — CLAUDE.md
  §1, önce sorulur).
  **OP-5 5. tur notu (2026-09-26):** bu seçenek bir `pg_db_role_setting` satırıdır.
  **Benimsenirse** ters katalog pini (`TestOperator00026_ReverseCatalogPin`, operatör
  rolünde rol düzeyi ayar reddi) ve runbook'un 2. adımı (`deploy/README.md` → "Operator
  roles (M10 OP-5)", `role_settings` sütunu, *"0 değilse DUR … RESET ile kaldır"*) **aynı
  değişiklikte** güncellenir ve izin verilen satır(lar) adıyla listelenir — yoksa runbook'u
  izleyen biri seçeneği siler. Ve tek başına yeterli değildir: `log_parameter_max_length`
  bağlı parametreleri yalnız **ifade** log'unda keser; hata bağlamındakileri
  `user` bağlamlı `log_parameter_max_length_on_error` yönetir (aşağıda, koşul 1) — o
  ayrıca iğnelenmedikçe reddedilen çağrıların parametreleri yine yazılır.
  **Üretimin bugünkü log ayarları — orkestratör ölçtü, salt-okunur, 2026-09-26:**
  `log_statement = none` · `log_min_duration_statement = -1` ·
  `log_parameter_max_length = -1` · `log_parameter_max_length_on_error = 0` ·
  `log_min_error_statement = error` · `log_min_messages = warning` ·
  `log_error_verbosity = default`. Bu değerlerle hiçbir `op_*` parametresi sunucu
  log'una girmez; güvenliği iki **adsız** koşula dayanır, ikisi de burada adlandırıldı:
  (1) 🔴 **`log_parameter_max_length_on_error` 0 KALMALI — ve bunu işletme TEK BAŞINA
  garanti EDEMEZ (4. tur düzeltmesi).** Her `op_*` reddi bir ERROR'dır ve
  `log_min_error_statement = error` o ifadeyi (STATEMENT) log'lar; bu ayar 0 **değilse**
  (pozitif **ya da `-1`** — `-1` "sınırsız"dır) reddedilen çağrının **bağlı
  parametreleri** — ham enrollment token'ı, oturum token'ının hash'i, digest, zarf — hem
  sunucu log'una (pod log'u; güvenlik denetçisi ölçtü) hem çağıranın hata `CONTEXT`'ine
  düşer (4. turda ölçüldü, `\bind` ile genişletilmiş protokol, reddedilen
  `op_touch_session($1)`: `0` → yalnız fonksiyonun satırı; `-1` → ek satır
  `unnamed portal with parameters: $1 = '…'`). Ayarın
  `pg_settings.context`'i **`user`**'dır (ölçüldü; `log_parameter_max_length`'inki
  `superuser`): DSN sahibi onu kendi oturumunda çevirir (ölçüldü: `tappa_operator`
  olarak `SET log_parameter_max_length_on_error = -1` → `-1`) **ve rol varsayılanı
  olarak yazar** (ölçüldü: `tappa_operator` olarak `ALTER ROLE tappa_operator SET
  log_parameter_max_length_on_error = -1` başarılı, `pg_db_role_setting`'de 1 satır) —
  varsayılan parola döndürülmesinden sağ çıkar ve havuzun her yeni bağlantısına
  uygulanır. **OP-7'ye, adıyla:** operatör havuzu `log_parameter_max_length_on_error = 0`
  (gerekiyorsa `log_parameter_max_length` da) bağlantı **başlangıç parametresi** olarak
  iğneler — başlangıç paketi rol varsayılanını ezer; OP-7 ölçer — ve açılışta
  `current_setting` 0 değilse havuzu açmayı reddeder. Bugünkü ucuz pin: ters katalog
  testi `pg_db_role_setting`'de iki operatör rolü için satır olmamasını ister ve runbook'un
  doğrulama sorgusu bu sayıyı gösterir. Aynı düğme `tappa_app` için de açıktır (kapsam
  dışı; kart madde 33). → **OP-7'de uygulandı (2026-10-01), İKİ havuza birden (backlog
  T79), ölçülerek:** `log_parameter_max_length_on_error = 0` her bağlantının başlangıç
  parametresidir ve bugünkü kodda **her yeni bağlantıda** geri okunur (ölçen: `TestPin_AConnectionTheParameterDidNotReachIsRefused`; `internal/db/logparams.go`; ilk
  bağlantı açılışın ping'idir, yani reddi açılış reddidir). Ölçüm (dev, rol varsayılanı
  `tappa_app IN DATABASE postgres` için -1, sonra RESET): parametresiz bağlantı -1, pinli 0;
  DSN'deki `options=-c …=-1` ve `?…=-1` pini yenemedi — *(2. tur: harf büyüklüğü farklı bir
  anahtar, `?LOG_PARAMETER_MAX_LENGTH_ON_ERROR=-1`, ilk hâlde pgx'in haritasında ikinci bir
  anahtar olarak kalıyor ve paket sırasına göre kazanıyordu — 30 açılışta 7–11 ret, ölçüldü;
  pin artık o adı harf duyarsız eşleyen her anahtarı siler, 30/30 açılış 0)*. `log_parameter_max_length`
  **iğnelenmedi**: `superuser` bağlamlıdır, süper kullanıcı olmayan bir başlangıç paketi onu
  adlandırınca bağlantı **reddedilir** (42501, ölçüldü) ve aynı sebeple DSN sahibi de onu
  değiştiremez; üretim onu -1'de koşturduğu için geri okuma onu şart koşmaz.
  `TestPin_NoRoleLevelSettingOnTheConnectingRoles` artık `tappa_app` için de satır
  olmamasını ister. *(2c, güvenlik denetimi, ölçüldü: pin ve geri okuma tek başına bu
  koşulu sağlamıyordu. DSN'deki `default_query_exec_mode=simple_protocol`'u pgx kendisi
  okur, sunucuya göndermez ve argümanları SQL metnine istemci tarafında gömer; iki havuz
  açıldı, geri okuma "0" dedi ve nöbetçi değer `current_query()`'deydi — yani hata veren
  ifadenin STATEMENT satırında. Artık iki havuzun kurucusu yalnız bağlı parametre gönderen
  kipleri kabul eder (`boundParameterModes`: cache_statement, cache_describe,
  describe_exec, exec — beşinin her koşuda ölçüldüğü liste; `simple_protocol` reddedilir,
  normalleştirilmez). "OP-7'de uygulandı" iddiası bu ret ile birlikte doğrudur.)* (2) **
  `log_min_duration_statement` açılırsa** `log_parameter_max_length = -1` ile yavaş bir
  `op_*` çağrısının parametreleri **tam** log'lanır; açılacaksa önce
  `log_parameter_max_length = 0`. Ve bugünkü güvenlik **Go'nun bağlı parametre
  kullanmasına** dayanır: SQL metnine gömülen bir değer `log_statement` / hata STATEMENT'ı
  yoluyla parametre ayarlarından bağımsız log'lanır. **OP-7'ye, adıyla:** operatör
  sorguları yalnız bağlı parametreyle yazılır, SQL metnine değer gömülmez. *(2c: bu koşul
  kaynak kodla sınırlı değildi — bir DSN parametresi (`default_query_exec_mode=simple_protocol`)
  ya da çağrı başına bir `pgx.QueryExecModeSimpleProtocol` argümanı pgx'in bütün
  argümanları metne gömmesini sağlar. DSN yolu ~~kapandı~~: kurucular o kipi reddeder
  (`TestPin_ADSNCannotChooseClientSideInterpolation`). ~~Ürün kodunda o argüman yoktur ve
  yazılamaz (AST taraması).~~ → *2d, kapanış denetimi, ölçüldü:* ada bağlı tarama dört
  yazımı görmedi (dot import, `pgx.QueryExecMode(5)`, `pgx.QueryExecModeExec + 1`,
  `…DefaultQueryExecMode = 5`) ve çağrı başına `pgx.QueryExecMode(5)` nöbetçiyi
  `current_query()`'ye koydu. ~~Kural artık TİPE bağlı ve kapalı: modülün ürün Go kodunda
  tipi `github.com/jackc/pgx/v5.QueryExecMode` olan HİÇBİR ifade ve `DefaultQueryExecMode`
  alanının hiçbir kullanımı `internal/db/logparams.go`'nun iki bildirimi
  (`boundParameterModes`, `requireBoundParameters`) dışında geçemez~~ (tip taraması
  `go/types` ile, bütün bağımlılıkların tam export verisiyle; 34 paket, 207 test dışı
  dosya; pozitif kontrollü — *2f: artık bir TUZAK TELİ, kapalı kural değil; aşağıdaki
  2f notu*). ~~**Kalan tek
  sınır, adıyla:** reflection ya da `unsafe` — bu tipte bir ifade yazmadan değer yazan yol.~~
  → *2e, 2. kapanış denetimi, ölçüldü: o iddia da geniş çıktı — build kısıtlı bir ürün
  dosyası (`//go:build !race` ya da `!cgo`) testin koştuğu build'de yoktu, üretimin
  build'inde (CGO_ENABLED=0, -race yok) vardı; ve kurucunun içinde, kontrolden SONRA
  değiştirilen bir kip hiçbir testi kırmızıya çevirmedi. Katman değişti — üç katman ve bir
  davranış ölçümü: (i) kurucular DSN'i bağlanmadan önce reddeder; (ii) HER YENİ BAĞLANTI
  kendi kipini yeniden denetler (`pinLogParameters`'ın `AfterConnect`'i) — ~~kurucunun
  geri kalanında ya da havuzun config'inde sonradan yapılan değişiklik bağlantıya
  ulaşamaz~~ *(2g, 4. kapanış denetimi, ölçüldü: koşulsuz değil — pin'den sonra havuzun
  config'ini baştan değiştiren bir dal (`cfg.IsProd()`'a ya da kullanıcı adına bağlı)
  `AfterConnect`'i de götürdü ve bütün testler yeşil kaldı. Ölçülene eşit cümle: bugünkü
  kurucular düz çizgidir ve kaynakları token token pinlidir
  (`TestConstructors_BodiesAreTheReviewedOnes`); `pinLogParameters`'ın `AfterConnect`'i
  havuzun kancası olarak kaldıkça havuzun config'inde sonradan yapılan bir kip değişikliği
  bağlantıda reddedilir; havuz kancalarına (`AfterConnect`, `BeforeConnect`, `ConnConfig`)
  `pinLogParameters` dışında ~~yazmak~~ *(2h: telin listelediği yazım biçimleriyle yazmak —
  `poolConfigHooks`'un yorumu)* bir teli tetikler; davranış testi müşteri havuzunu
  dev VE üretim ortamıyla koşar)*; (iii) kaynak: tip taraması, artık `go list`'in modül paketleriyle BİREBİR aynı
  kümede, koşan build'in dışarıda bıraktığı her test dışı dosyada kırmızı
  (`IgnoredGoFiles`, `CgoFiles`, `IgnoredOtherFiles`), ve ortamdan bağımsız bir kural
  ürün dosyalarında build kısıtı, GOOS/GOARCH dosya adı eki, cgo ve go'nun yok saydığı
  ad yasaklar (`TestProductCode_CarriesNoBuildConstraint`; bugün böyle dosya 0, liste
  boş); ve DAVRANIŞ: iki kurucunun döndürdüğü havuzda üç ayrı bağlantıda nöbetçi
  argüman `current_query()`'de yok (`TestPools_KeepArgumentsOutOfTheStatementText`).
  ~~**Kalan, adıyla:** çağrı başına reflection ya da `unsafe` ile yazılmış bir kip (havuz
  çapında olanı bağlantı katmanı ve davranış testi yakalar); ve kod içinde SQL kurmak
  (`QueryRewriter`, `fmt.Sprintf`) — bu kuralın konusu değil (CLAUDE.md §6; `operator.go`'nun
  bağlı-parametre pini).~~ → *2f, 3. kapanış denetimi ve orkestratör kararı:* o "kalan"
  listesi de geniş çıktı — `pgxtest.AllQueryExecModes` + çıkarımlı bir generic çağrı
  başına `simple_protocol` seçti; reflection yok, `unsafe` yok, kip tipinde bir ifade
  yazılmadı. **İddianın biçimi değişti.** (1) **Havuz düzeyi, ölçülü** *(2h: "KAPALI" sözcüğü
  kaldırıldı; cümle bugün sevk edilen kod hakkındadır)*: sevk edilen
  iki havuzun hiçbir bağlantısı argümanı metne gömen bir varsayılan kiple çalışmaz
  (kurucuların DSN denetimi, bağlantı başına denetim, davranış testi) *(2g: bu cümlenin
  taşıyıcıları ölçülene eşitlendi — yukarıdaki (ii) notu: kurucular düz çizgi ve token token
  pinli, bağlantı başına denetim `pinLogParameters`'ın `AfterConnect`'i kanca kaldıkça,
  havuz kancalarına yazma teli, davranış testi müşteri havuzunu dev ve üretim ortamıyla
  koşar; operatör havuzunda üretim yolu ile test kancası yolu arasındaki tek fark — *2h:
  pinli bildirimlerin içinde* — `before`'dur ve kaynak pinlerinde görünür)*. (2) **Çağrı başına
  kip için tamlık iddiası yoktur.** Kod içinde SQL kurmakla aynı sınıftır; taşıyan kurallar
  CLAUDE.md §6 ve `operator.go`'nun bağlı-parametre pinidir. Tip taraması
  (`TestProductCode_ExecModeWireAndConnectWire`) bir tuzak telidir ve yalnız listelediği
  yazımları yakalar: dot import, dönüşüm, aritmetik, alana untyped sabit, adlı sabit,
  generic örneğin tip argümanı, alias, `any`'den tip iddiası, literal anahtarı, kipten
  kurulu bileşik tipler (dilim, dizi, map, kanal, işaretçi, imza, demet) ve
  `pgxtest`'in listesi; yakalamadığı her yol (örnek: bilmediği bir kaynaktan beslenen
  çıkarımlı generic, reflection, `unsafe`, `QueryRewriter`/`fmt.Sprintf`) kod incelemesinin
  ve §6'nın konusudur. (3) **Kapalı ve yapısal:** üretim ikilisinin bağımlılık kapanışı
  `testing`'i ve `pgxtest`'i içermez (`TestBinary_LinksNoTestCode`) — bu turun kaçışındaki
  değer kaynağı yapısal olarak kesik.)* *(2h, 5. kapanış denetimi ve orkestratör kararı —
  İDDİANIN BİÇİMİ KALICI OLARAK: beş kapanış turu her pinin dışında bir yer buldu, çünkü
  metin GELECEKTEKİ KEYFİ kod değişikliklerine karşı bir garanti gibi okunuyordu; hiçbir
  test bunu kanıtlayamaz. Bu notun üstündeki OP-7 2c–2g notları şu üç parçaya göre okunur ve
  yalnız bunları söyler.* **(I) Bugün sevk edilen kod, ölçülen davranış:** iki havuzun her
  bağlantısı bağlı parametre gönderen bir kipte çalışıyor ve
  `log_parameter_max_length_on_error` 0'a pinli — ölçen testler:
  `TestPools_KeepArgumentsOutOfTheStatementText` (iki havuz; müşteri havuzu dev ve prod),
  `TestPin_ADSNCannotChooseClientSideInterpolation` (DSN reddi, bağlanmadan önce),
  `TestPin_AModeChangedAfterTheCheckIsRefusedOnItsConnection` (bağlantı başına ret),
  `TestQueryExecModes_OnlyTheListedOnesBindOnTheServer`, ve pin ile geri okuma
  (`TestPin_TheStartupParameterOverridesARoleDefault`, `TestPin_ADSNCannotUnpinIt`,
  `TestPin_ADifferentlyCasedKeyCannotUnpinIt`, `TestPin_AConnectionTheParameterDidNotReachIsRefused`,
  `TestPin_AReadBackThatCannotRunIsRefused`, `TestLogParameterPinned_OnlyTheTextZero`).
  **(II) Adıyla sayılan pinler ve teller, tam liste:** `TestConstructors_BodiesAreTheReviewedOnes`
  — `newDB`, `openOperatorDB`, `pinLogParameters`, `requireBoundParameters` gövdeleri ve
  `boundParameterModes` başlatıcısı, token token; `TestConstructors_TheHookReachesOnlyThePin`
  — `New` ve `NewOperatorDB` gövdeleri, kancanın yalnız `pinLogParameters`'ın üçüncü argümanı
  olması; `TestProductCode_ExecModeWireAndConnectWire` — KİP teli (`execModeEscapes`'teki
  yazımlar; izinli bölgesi `logparams.go`'da `boundParameterModes` ya da
  `requireBoundParameters` adlı PAKET DÜZEYİ bildirim, dosyanın kendi konumuyla — aynı adlı
  yöntem ve `//line` yönergesi sayılmaz (2i) —, izinli kullanım tam olarak 16), BAĞLANTI teli
  (`connectFuncs`, iki kurucu dışında), HAVUZ-CONFIG teli (`poolConfigHooks` yorumundaki
  yazım biçimleri: alana atama, alan üzerinden atama, range ataması, artırma, adres alma,
  literal anahtarı, `pgxpool.Config`'in ya da onun alttaki struct'ını taşıyan bir tipin
  bütün değeri — 2i); kapalı
  yapısal kurallar `TestBinary_LinksNoTestCode` (ikili kapanışı, linux/amd64, cgo=0: `testing`
  ve `pgxtest` yok) ve `TestProductCode_CarriesNoBuildConstraint` (orada listelenen kısıt
  türleri). **(III) Tamlık iddiası yok:** pinlerin ve tellerin listelemediği her kod
  değişikliği — pinsiz yardımcılar (ör. `requireLogParametersPinned`, `logParameterPinned`,
  `readRole`), importlar, başka başlatıcılar, ortama koşullu davranış, reflect ve `unsafe`,
  çağrı başına seçilen kip — kod incelemesinin konusudur. (Geliştirme
  veritabanı bunun tersidir: `log_statement = all` ve bağlı parametreler log'da — yalnız
  yerel.)
- Bilet tablosunun adı — tablo OP-5'in tablolarıyla birlikte doğar. (Karara bağlananlar:
  ömür tavanı 60 sn; ham biletin boyu 256 bit — §2 v 1'in geri çevrilemezlik argümanı bu
  boya dayanır.) → **OP-5 (2026-09-26): `operator_read_tickets`.**
- `platform_*` tablolarında gönüllü RLS (ADR 0016 §1 emsali) — OP-5. → **OP-5'te karara
  bağlandı (2026-09-26): dört tabloda da ENABLE + FORCE**, tek politika (yukarıda).
  Gerekçe: geri yüklemenin varsayılan ACL kalıntısı (M8-02 FAZ E) RLS'le 0 satır okur;
  FORCE çünkü `scripts/pg-restore-verify.sh` ENABLE ve FORCE sayıları farklı bir
  veritabanını reddeder.
- Ek kemer olarak `TEMP` hakkının PUBLIC'ten alınması (§2 iv'ün sondasını bütün roller
  için kapatır ama bütün rolleri etkiler) — değerlendirilmedi.
- A2'nin fatura ve askı fonksiyonlarına bağlı ⏳ sorular: **OP-K5** (askıdaki ayın
  faturalanması), **OP-K12** (T27/T37 plan geçmişi) — m10-platform.md §6.

## OP-5 uygulama notu (2026-09-26)

Uygulama: `db/migrations/00026_create_platform_operator.sql` +
`scripts/db-init/01-roles.sql` (işaretli **OPERATOR ROLES** bloğu) + `deploy/README.md` →
*"Operator roles (M10 OP-5)"*. Kararın gövdesi değişmedi; uygulamanın bu metne eklediği ya
da onu keskinleştirdiği yerler, adıyla (ayrıntı ve ölçümler:
[m10-platform.md](../plan/m10-platform.md) → OP-5 kart düzeltmesi):

- **`op_touch_session` audit satırı YAZMAZ.** Yazar (`last_used_at`) ama her `op_*` onu
  çağırdığı için (§2 i) bir satır, kabul edilen her eylemin arkasına iki satır koyardı ve
  OP-11'in *"kabul edilen her okuma tam 1 satır"*ını kırardı. Diğer dördü kabul edilen her
  çağrıda tam bir satır yazar. Dönüşü `OUT session_id uuid, OUT admin_id uuid` — tek satır.
- **`op_record_auth_event(p_kind, p_email DEFAULT NULL, p_admin DEFAULT NULL)`.** §1'in
  *"aldığı adresi arar"*ına ek: adres verilmezse hesap **id** ile aranır (`totp_failed`,
  `locked`, `enrollment_failed` Go'nun bir id tuttuğu yerlerde doğar). Var olmayan id FK
  hatası değildir — hedef `NULL`, varlık kehaneti yok.
- **Her `op_*` reddi SQLSTATE 28000**, fonksiyon başına tek mesaj; mesaj hiçbir argümanı
  biçimlendirmez. **Düzeltme (2. tur):** ilk hâlinde bu cümle doğru değildi — argümanı
  reddeden bir kısıt (bcrypt şekli, zarf boyu, oturum hash'inin şekli ya da tekrarı)
  PostgreSQL'in DETAIL satırıyla yazılan **değerleri** (digest, zarfın hex'i, adres,
  enrollment hash'i) çağırana ve sunucunun stderr'ine döndürüyordu; ve 23514/23505 yalnız
  bütün koşullar geçince çıktığı için bir "koşullar geçti" kehanetiydi. Argümanı bir
  kısıta ulaşan iki fonksiyon (`op_open_session`, `op_complete_enrollment`) artık
  `integrity_constraint_violation`'ı yakalayıp **aynı** 28000'e çevirir; geride yalnız
  kısıt adını ve SQLSTATE'i taşıyan bir LOG satırı kalır (dev'de ölçüldü: 14 yakalanan ret,
  sunucu log'unda 0 *"Failing row contains"*). **3. tur düzeltmesi:** o satır "yalnız
  sunucuda" değildir ve kehanet **kapanmadı** — `client_min_messages = log` diyen çağırana
  da döner ve yalnız koşullar tuttuğunda doğar; kapanan şey değer sızıntısı ve DETAIL'dir,
  "koşullar tuttu" biti sayılı sınır 14'tedir (SAVEPOINT kanalıyla aynı bilgi). Şekil ön kontrolü yerine seçildi: ön kontrol
  her CHECK'in ikinci bir kopyası olur ve tekrar eden hash'i (23505) yarışsız
  kapsayamaz. `TestOperator00026_ArgumentsNeverComeBackInAnError` pinler. Go tarafı
  `PgError.Detail`'i asla log'lamaz (OP-7).
- **Tek kullanım iki bağımsız katmandır (2. tur):** fonksiyonun `enroll_used_at IS NULL`
  yüklemi ve şemanın `CHECK (status <> 'pending' OR enroll_used_at IS NULL)`'ı; her biri
  ayrı testle pinli. `reset-mfa` token hash'ini değiştirir ve `enroll_used_at`'i aynı
  ifadede `NULL` yapar (OP-9).
- **Ön koşul `tappa_opdefiner`'ın KENDİ üyeliklerini de sınar (2. tur):** transaction
  içinde `GRANT tappa_owner TO tappa_opdefiner` önceki ön koşulu geçiyordu ve definer
  `tags.aes_key_ref` ile `admin_users.password_hash`'i okur hâle geliyordu (ölçüldü).
  **3. tur:** `tappa_operator`'ın **üyelerini** de sınar — `GRANT tappa_operator TO
  tappa_app` ön koşulu geçiyordu ve `tappa_app` `op_record_auth_event`'i çağırdı
  (ölçüldü); dört üyelik yönünün dördü de artık reddedilir.
- **§6'nın "asla" sütunları ve definer'ın tenant erişimi pinli (3. tur):** yedi sır
  sütununda `tappa_opdefiner` SELECT'i `false`; OP-5 itibarıyla definer'ın dört operatör
  tablosu dışındaki hiçbir tabloda ve dizide yetkisi yok (`TestOperator00026_PrivilegeMatrix`).
  OP-10+ bilinçli grant eklerken testin izin listesini tam sütun listesiyle, aynı
  değişiklikte genişletir; "asla" sütunları o listeye giremez.
- **Sayaç yalnız `active` hesapta ilerler (2. tur):** filtresiz `UPDATE` `pending`/
  `disabled` bir satırı da kilitliyordu; açık tutulan bir çağrı ikinci çağıranı
  bekletiyor (`55P03`), bilinmeyen id anında dönüyordu — giriş aramasının gizlediği
  hesaplar için bir kilit-çekişmesi kehaneti. Aktif hesaplar bu yoldan hâlâ görünür;
  `tappa_operator` onları zaten SELECT edebilir. *(4. tur: bu yalnız **kilit** kanalını
  kapattı; aynı gizli hesapların varlığı istatistik görünümlerinden hâlâ okunur — sayılı
  sınır 15.)*
- **Kilit şekli bilinçli (2. tur):** sayaç **yalnız başarıda** sıfırlanır; eşiğe bir kez
  ulaştıktan sonra her yeni hata tam pencereyi yeniden açar — pencere geçtikten sonra tek
  hata yeniden kilitler, kilitliyken hata pencereyi uzatır. İlk kilitten sonra pencere
  başına en çok bir tahmin (15 dk'da günde 96); pencere sonunda sayacı sıfırlamak N
  tahmin verirdi (günde 480). Testle pinli.
- **Operatör hedef tenant'ı `target_tenant_id`**, `tenant_id` değil (o ad tabloyu depo
  genelindeki türetimlerde tenant verisi yapardı); FK yok (B14).
- **Kilit sayıları geçici: N = 5, 15 dk** — mekanizma sayısız var olamazdı; kesin sayılar
  OP-6/OP-8 (ADR 0020 "Karar verilmedi").
- **00026'nın ön koşulu** rollerin varlığını, §1'in niteliklerini ve üyeliklerini sınar;
  eksikse runbook'un adını mesajda taşıyarak düşer (goose HINT basmaz). Dev'de rollerle ve
  rolsüz ölçüldü.

## OP-10 uygulama notu (2026-10-03, A fazı — veri katmanı)

Uygulama: `db/migrations/00027_move_legal_publishing_to_the_operator.sql` +
`internal/db/operator.go` (dışa açık `LegalVersions`, `PublishLegal`; paket içi
`beginOperatorRead`, `readLegalVersions`, `readTicket`) + `db/queries/operator.sql` (belge) +
`internal/db/operatorlegal_test.go`. Handler, ekran, wiring ve panelin izin listesinin
kaldırılması B fazıdır. Kararın gövdesi değişmedi; uygulamanın karar verdiği ya da
keskinleştirdiği yerler, adıyla (ayrıntı ve ölçümler: [m10-platform.md](../plan/m10-platform.md)
→ OP-10A kart düzeltmesi):

1. **`op_begin_read(p_session text, p_kind text, p_params jsonb) RETURNS text`** — iki
   aşamalı okumaların ortak ilk aşaması. Okuma türleri kapalı bir kümedir (bugün tek üye:
   `legal_versions`) ve iki yerde durur: fonksiyonun kendisi (bilinmeyen tür → 22023) ve
   `operator_read_tickets_kind_check` (OP-5'te şekildi — OP-5 kart düzeltmesi md. 14 c —,
   artık kapalı küme). Her tür parametre anahtarlarını **tam** adlandırır (eksik, fazla ya da
   yanlış tipte anahtar → 22023). Hash'lenen metin çağıranın JSON yazımı değil, fonksiyonun
   tipli değerlerden **yeniden kurduğu** nesnenin jsonb metnidir; `op_read_*` aynı nesneyi
   kendi tipli argümanlarından kurar. Sonraki `op_read_*` iki listeyi kendi migration'ında
   genişletir (`CREATE OR REPLACE`).
2. **Audit türü `read`, okunan şey `target_scope`'ta.** Kabul edilen bir `legal_versions`
   okuması `op_begin_read`'de tek bir `read` satırı yazar (oturum başına sayıldı:
   `TestOpReadLegalVersions_TwoPhaseLifecycle`); neyin okunduğu `target_scope` (= okuma
   türü) ve içeriksiz `page_number`/`page_size`'tır. Yeni CHECK
   `operator_audit_log_read_has_scope`: `kind = 'read'` ⇒ `target_scope` dolu. Böylece
   OP-11 audit türü değil okuma türü ekler. Yayının türü `legal_publish`; `detail` =
   `{slug, document_id, bytes}` — metin değil (`internal/domain/legal.PublishedDetail`
   emsali). Yasal belge hiçbir tenant'ın değildir; tenant `audit_log` satırı yazılmaz (ADR
   0020 "Audit'in yeri").
3. **Ham bilet:** 64 küçük harf hex — üç `gen_random_uuid()`'nin (çekirdek
   `pg_strong_random`, 366 bit) SHA-256'sı. `pgcrypto`'nun `gen_random_bytes`'ı
   **seçilmedi**: 00027'den önceki migration'larda `gen_random_bytes`, `digest(`, `crypt(`,
   `gen_salt` için 0 eşleşme (ölçüldü, `grep`); uzantı `scripts/db-init/01-roles.sql`'de
   yaratılıyor, migration'da değil; bu migration o bağımlılığı eklemiyor. **Ömür 30 sn**
   (OP-5 md. 14 a: 60'tan kesin küçük); sınır 4'ün penceresi de budur.
4. **`legal_documents.published_at` DEFAULT `clock_timestamp()`** (§2 vii) ve sütun
   tanımlayıcının INSERT listesinde **yok**. K6'nın `audit_log.at` için seçtiği "açıkça
   yaz" yolu yerine 00026'nın biçimi seçildi: yazılabilir bir zaman sütunu tanımlayıcıya
   istediği zamanı yazdırır (ADR 0015 emsali, O-2) ve DEFAULT katalogda taranabilir.
   Herkese açık okuma yolunun eşitlik kırıcı yorumu (`db/queries/legal.sql`) aynı
   değişiklikte güncellendi.
5. **`op_publish_legal(p_session, p_slug, p_body) RETURNS void`:** oturum
   `op_touch_session`'dan; `published_by` oturumun operatörü (aktör parametresi yok — imza
   pinli); sürüm ve `legal_publish` satırı **tek ifade**. 256 KiB =
   `octet_length(body) > 262144` → 22023. **Ölçülerek eklenen:** 00020'nin CHECK'i
   `btrim(body) <> ''` yalnız BOŞLUK kırpar; satır sonu ve sekmeden ibaret bir gövde
   CHECK'ten geçer (dev'de ölçüldü: `btrim(E'  \n\t ') <> ''` = `t`). Fonksiyon
   `[:space:]` dışında karakter taşımayan gövdeyi reddeder. **Bu sınıf locale'e bağlıdır**
   (2026-10-03, ikinci tur, dev: `datctype = en_US.utf8`, libc): reddedilen U+0009–000D,
   U+0020, U+0085, U+2000, U+2003, U+2028, U+2029, U+205F, U+3000; kabul edilen U+00A0,
   U+1680, U+180E, U+200B, U+200C, U+200D, U+202F, U+2060, U+2800, U+3164, U+FEFF.
   Üretimin ctype'ı ölçülmedi. Go'nun `strings.TrimSpace`'i bunlardan U+00A0, U+1680 ve
   U+202F'yi de kırpar; U+180E, U+200B, U+200C, U+200D, U+2060, U+2800, U+3164, U+FEFF'yi
   ne bu kontrol ne `TrimSpace` kırpar (ölçüldü, Go 1.26.7) — boş görünen bir yasal sayfa
   yayımlanabilir (güvenilen operatör girdisi; bütünlük sorunu). Görünür karakter
   denetimi B'nin handler'ına devredildi (kart, B devri). Kısıta ulaşan argümanlar
   (kapalı küme dışı slug, NULL slug ya da gövde) 00026 §5 kalıbıyla yakalanır: tek 22023,
   DETAIL yok — yakalanmasaydı "Failing row contains (…)" 256 KiB'a kadar gövdeyi sunucu
   log'una taşırdı.
6. **Hata sınıfları:** 28000 = ölü oturum (`op_touch_session`'ın kendi mesajı) ya da
   reddedilen bilet/okuma; 22023 = argüman reddi, beş dal (00027 §4'ün numaralarıyla):
   (1) `op_begin_read`'in adlandırmadığı okuma türü ya da parametre nesnesi, (2) sınır dışı
   sayfa, (3) 256 KiB'ı aşan gövde, (4) yalnız `[:space:]` karakterlerinden oluşan gövde,
   (5) `legal_documents`'ın bir kısıtının reddettiği slug ya da gövde. Bu beş dal
   argümanın kendisine ve şemanın kısıtlarına bakar, okunan bir satıra değil (§2 v 7; kod
   okuması — 00027 §4).
7. **`op_begin_read`'in kısıt yakalayıcısı:** bilet satırının kendisi bir kısıta takılırsa
   (testte zorlandı) DETAIL'in satırı bilet hash'ini taşırdı — §3.5'e göre bilet ham da
   hash'li de log'a girmez. Yakalanır; sabit 28000 `op_begin_read: read refused`, audit
   satırı da geri alınır.
8. **Sürüm listesinin sütunları:** `version_id, slug, published_at, body_bytes,
   publisher_kind ('operator' | 'legacy'), publisher_admin_id, publisher_name,
   is_current`. Eski bir satırın (M7-06 paneli: müşteri `admin_users.id`'si ya da NULL)
   yayımlayan kimliği **döndürülmez**; ayrım `platform_admins`'e JOIN'le yapılır (iki ayrı
   rastgele v4 uzayı) ve yeni sütun eklenmedi. Gövde döndürülmez (200 × 256 KiB); listede
   uzunluğu var. Sıralama herkese açık okumanınkiyle aynı (`published_at DESC, id DESC`),
   LIMIT gövdede `least(sayfa, 200)`.
9. **Tanımlayıcının yeni yetkileri:** `legal_documents` SELECT (id, slug, body,
   published_at, published_by) + INSERT (slug, body, published_by); `operator_audit_log`
   SELECT (id) (OP-5 md. 14 b); `platform_admins` SELECT (display_name). OP-5 uygulama
   notunun kuralıyla `TestOperator00026_PrivilegeMatrix`'in izin listeleri aynı
   değişiklikte, tam sütun listesiyle genişletildi; "asla" sütunlarından hiçbiri listede
   değil.
10. **00026'nın ön koşulu 00027'de tekrarlandı** (aynı roller, nitelikler, dört üyelik
    yönü): üç yeni `SECURITY DEFINER` fonksiyon `tappa_opdefiner`'a ait olacak.
11. **Down:** `tappa_app`'in INSERT'ini geri verir (00020'nin sütun grant'ı — bilinçli bir
    güvenlik gerilemesi, 00026 durumu) ve audit tür CHECK'ini, yeni türden satır VARSA
    `NOT VALID` geri koyar — o satırlar ek-yalnız tablodaki kanıttır. Ölçüm (dev): `read`
    satırı yokken Up → Down şeması 00026'yla birebir, Up → Down → Up birebir; satır varken
    Down şemasının tek farkı `NOT VALID`.
12. **Eşzamanlı görünmezlik, ölçülerek:** A'nın açık ilk aşamasının bileti başka bir
    bağlantıdan görünmez (sahibin sayımı 0, A'nınki 1); **aynı oturumla** okuyan B veri
    almaz — `op_touch_session`'ın oturum satırı kilidinde **bekler** ve A geri alınınca
    28000 alır. (Testin ilk hâli anında ret bekledi ve son süresine kadar asılı kaldı.)
13. **Go tarafı:** iki dışa açık serbest fonksiyon; `OperatorConn`'a `Query` eklendi.
    `*OperatorDB`'ye **yöntem eklenmedi**: `TestOperatorDB_IsTheStoreAndNothingMore` yöntem
    kümesini `operatorauth.Store` + `Close`'a pinler; tüketici (B'nin handler'ı) arayüzünü
    yazdığında küme ve pin birlikte değişir. Bilet paket dışına çıkmaz (`readTicket`,
    `SealedSecret`'ın beş yöntemli kalıbı).
14. **Ölçülüp alınmayan:** `legal_documents` politikasını `WITH CHECK (false)` ile daraltmak
    (geri yükleme kalıntısı bir INSERT'e karşı ikinci kemer). Alınmadı: Down'un 00020'nin
    `WITH CHECK (true)`'sunu geri yazması `scripts/redline-check.sh` R5b'nin izin veren
    politika (totoloji) kuralına takılır — muafiyet yalnız
    `CREATE POLICY legal_documents_public` yazımına bağlı —, ve geri yükleme kalıntısı
    `scripts/pg-restore-verify.sh`'in tablo ve sütun düzeyi yetki karşılaştırmasıyla
    raporlanır.
15. 🔴 **A fazı B fazı olmadan `main`'e gitmemeli.** 00027 deploy edilip B gelmezse panelin
    `/admin/legal` POST'u `Store.Publish`'in 42501'iyle 500 döner (`problemLegalUnavailable`,
    `internal/handler/legaladmin.go`'nun `default` dalı) ve operatörün yasal metin ekranı
    yoktur; o arada bir yasal metin ancak `tappa_owner`'ın SQL'iyle eklenebilir — bir
    `legal_publish` satırı bırakmadan ve sürüm listesinde `legacy` sınıflanarak.
    Birleştirme sırası bunu önler; bu not birleştirme kararının girdisidir.

**Güvenlik iddiası — üç parça.**

- **PART I — bugün sevk edilen kodun ölçülen davranışı** (dev Postgres 17.10, 2026-10-03;
  ölçen testin adıyla):
  - `tappa_app`: `has_any_column_privilege(…, legal_documents, INSERT) = f`, beş sütunun
    beşinde `has_column_privilege(…, INSERT) = f`, INSERT 42501; SELECT (id, slug, body,
    published_at) ayakta; `has_table_privilege`'ın kanıt olamayışı aynı testin kontrolünde
    ölçülür (sütun grant'ı geri verilince `has_table_privilege` = `f`, INSERT başarılı) —
    `TestOperator00027_TheApplicationCanNoLongerWriteALegalText`. Aynı test: `tappa_operator`
    `legal_documents`'ta SELECT/INSERT/UPDATE/DELETE/TRUNCATE'te yetkisiz, doğrudan SELECT
    ve INSERT 42501; `tappa_app` üç fonksiyonu çağırınca 42501.
  - Aynı transaction'da (üst düzey; açık ve bırakılmış savepoint) yaratılan bilet 28000 —
    `TestOpReadLegalVersions_ATicketFromThisTransactionIsRefused`. Commit sonrası veri;
    geri alınan okumada audit kalıcı ve bilet yeniden okunur (sınır 4); commit edilmiş
    tüketimden sonra 28000; başka sayfa, başka oturum ve iptal edilmiş oturum 28000;
    oturum başına tam 1 `read` satırı — `TestOpReadLegalVersions_TwoPhaseLifecycle`.
    Eşzamanlılık — `TestOpReadLegalVersions_AnUncommittedTicketIsInvisibleToAnotherSession`.
    Süre (dolmuş; açık transaction'da savepoint ×3 ve uykusu içinde tek `DO`) —
    `TestOpReadLegalVersions_ExpiryIsTheWallClock`. Sahtecilik —
    `TestOpReadLegalVersions_AForgedTicketIsRefused`. 200 tavanı, sıralama ve
    `body_bytes`'ın BAYT sayması (Maltaca metin) — `TestOpReadLegalVersions_PagesAreCappedAndOrdered`.
  - `op_begin_read`: tek audit satırı, saklanmayan bilet, 30 sn ömür —
    `TestOpBeginRead_WritesOneAuditRowAndStoresNoTicket`; ölü oturum ve kötü parametre
    reddi, satır yazılmaz — `TestOpBeginRead_RefusesDeadSessionsAndBadParameters`; kısıta
    takılan bilet satırı DETAIL'siz — `TestOpBeginRead_ARefusedWriteCarriesNoTicketHash`.
  - `op_publish_legal`: sürüm + audit birlikte, `published_by` = oturumun operatörü,
    `published_at` duvar saati — `TestOpPublishLegal_WritesTheVersionAndItsAuditRowTogether`;
    audit yazılamayınca sürüm de yok — `TestOpPublishLegal_AFailedAuditRowTakesTheVersionWithIt`;
    ölü oturumlar — `TestOpPublishLegal_RefusesEveryDeadSession`; argüman reddi ve 256 KiB
    sınırı — `TestOpPublishLegal_RefusesADocumentWithoutEchoingIt`; ek-yalnız —
    `TestOpPublishLegal_TheTableStaysAppendOnly`.
  - Havuzda iki aşama iki transaction, tek transaction'da `ErrOperatorRefused`, ve
    erişimciyle okunan 2. sayfa commit edilen audit satırında sayfa 2 olarak kayıtlı —
    `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions`; bilet tipi yer tutucu basar —
    `TestReadTicket_PrintsOnlyThePlaceholder`.
  - Şemanın iki kapalı kümesi adıyla: kapalı küme dışı bilet türü
    `operator_read_tickets_kind_check` ile, kapsamsız `read` satırı
    `operator_audit_log_read_has_scope` ile reddedilir — `TestOperator00026_TableShapeChecks`
    (2. tur eki); Down bilet tür CHECK'ini 00026'nın şekil kuralına, Up kapalı kümeye döndürür
    — `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`.
- **PART II — adıyla pinler ve yakaladıklarının tam listesi:**
  `TestOperator00027_TheThreeFunctionsAndTheirExactSignatures` — üç fonksiyonun tam argüman
  listesi, sonuç tipi, sahibi, adı başına tek overload, ileri katalog pininin bu üçüne dair
  bulguları, donan saat taraması ve `published_at` DEFAULT'u;
  `TestOperator00027_CallersTempTableIsNeverRead` — beş tablo adının (`legal_documents`,
  `operator_read_tickets`, `operator_audit_log`, `platform_admins`, `platform_sessions`)
  çağıranın geçici tablosuyla gölgelenmesi; `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`
  — Down'un iki dalı ve Up'ın geri alması; `TestOperator00027_PreconditionRefusesAWrongCluster`
  — dokuz rol şekli; `TestOperator00026_PrivilegeMatrix` (genişletildi) — tanımlayıcının
  tam sütun listeleri; `TestOperatorSQL_OnlyBoundParameters` (genişletildi) — on sabit ve
  Exec/QueryRow/Query çağrıları; **(2. tur)** `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays`
  — adı `op_read_` ile başlayan HER fonksiyonun kaynağında (katalogdan, adla türetilir):
  oturumun `op_touch_session` ile önce çözülmesi, tek tüketen UPDATE
  (`SET consumed_at = clock_timestamp()`), WHERE'inde altı koşul — hash (ham bilet ‖
  parametre nesnesi), `session_id = v_session`, `kind = '…'` (değeri
  `operator_read_tickets_kind_check`'in kümesinde), `consumed_at IS NULL`,
  `expires_at > clock_timestamp()`, `pg_xact_status(created_xact) = 'committed'` pozitif
  yazımıyla —, hemen ardından `IF NOT FOUND … 28000`, ve `RETURN QUERY` ondan sonra;
  yazım denetimi normalleştirilmiş metin üstünde düzenli ifadedir;
  `TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource` — `op_begin_read`'de
  `v_ticket`'ın TEK atamasının üç `uuid_send(pg_catalog.gen_random_uuid())` üzerinden
  SHA-256 olması, saklanan hash'in ondan hesaplanması, onun döndürülmesi ve
  `pg_catalog.gen_random_uuid()`'nin çekirdek (`internal`) fonksiyon olması. Mutasyon
  tablosu OP-10A kart düzeltmesinde.
- **PART III:** listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**OP-10 B fazı eki (2026-10-03).** Yukarıdaki md. 13'ün *"`*OperatorDB`'ye yöntem
eklenmedi"* cümlesi A fazının kaydıdır; B fazında tüketici yazıldı ve küme onunla değişti:
`*OperatorDB`'ye `LegalVersions(ctx, sessionHash, page)` ve `PublishLegal(ctx,
sessionHash, slug, body)` eklendi, ikisi de `return F(ctx, o.pool, …)`; tüketici arayüzü
`internal/handler/operator`'ın `LegalStore`'u. Pinler daraltılmadan güncellendi:
`TestOperatorDB_IsTheStoreAndNothingMore` kümeyi `operatorauth.Store` ∪
`operator.LegalStore` ∪ `Close` diye türetir (iki arayüz ortak ad taşırsa kırmızı),
`TestOperatorDB_EveryMethodDelegatesVerbatim` dokuz yöntem, `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor`
öncülü 10; `TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions` aynı okumayı
yöntemin kendisiyle, üretim kurucusunun havuzunda da sürer (bir `read` satırı daha commit
eder). Ekranın oturum argümanı `operatorauth.(*Authenticator).SessionHash`'tir
(`TestSessionHash_IsTheHashVerifyHandsTheStore`); okuma bileti `LegalStore`'un
imzalarında yok — bilet `internal/db.LegalVersions`'ın iki aşaması arasında kalır (kaynak
okundu). `PublishLegal` istek bağlamından kopuk bir bağlamla (`context.WithoutCancel`, 10 sn
zaman aşımı) çağrılır: istemcinin ayrılması `op_publish_legal`'i ve ardından gelen tazelemeyi
iptal etmez (`TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh`; sahte
store'da ölçüldü — sürücünün ifade ortasındaki iptale davranışı ölçülmedi). Ekranın ölçümleri ve üç parçalı
güvenlik iddiası: [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) §7, *"OP-10 B fazı
notu"*.

## OP-11 uygulama notu (2026-10-03, A fazı — veri katmanı)

Uygulama: `db/migrations/00029_read_tenants_from_the_operator.sql` + `internal/db/operator.go`
(dışa açık `TenantList`, `TenantDetail`, `TenantListQuery`, `TenantSummary`, `TenantOverview`,
`MaxTenantSearchRunes`, `ErrTenantSearchRefused`, `ErrNoSuchTenant`; paket içi `readTenants`,
`readTenantDetail`) + `db/queries/operator.sql` (belge) + `internal/db/operatortenants_test.go`.
Ekran, wiring ve `*OperatorDB` yöntemleri B fazıdır. **Bu, tenant verisini okuyan ilk `op_*`'tır** —
§3.2'nin *"op_* gövdesinde RLS YOKTUR"* cümlesinin ilk gerçek uygulaması. Kararın gövdesi
değişmedi; uygulamanın karar verdiği yerler, adıyla:

1. **Adlar §2 v 6'nın kuralıyla:** `op_read_tenants(p_session, p_ticket, p_query, p_page_number,
   p_page_size)` ve `op_read_tenant_detail(p_session, p_ticket, p_tenant_id uuid)` —
   m10-platform.md OP-11 satırındaki `op_list_tenants` / `op_tenant_detail` yerine.
2. **`op_begin_read` yerinde değiştirildi** (`CREATE OR REPLACE`; sahibi ve ACL'i korunur, migration
   yine de üçünü yazar): okuma türleri `legal_versions` (değişmedi), `tenants`
   (`{page_number, page_size, query}`, `query` en çok 254 karakterlik bir JSON dizgisi; `''` =
   bütün tenant'lar) ve `tenant_detail` (`{tenant_id}`, tireli uuid, iki harf büyüklüğünde de;
   hash'lenen metin uuid **değerinden** kurulur, yani küçük harfli kanonik metin). 🔴 **Birinci
   aşama tenant'ın varlığına BAKMAZ:** reddi iz bırakmaz (§2 v 9), yani var olmayan tenant'ı
   reddeden bir birinci aşama geri alınan bir çağrıya *"bu tenant var mı"* cevabı verirdi (B14'ün
   okuma hâli). Varlık ikinci aşamada, onu adlandıran `read` satırı commit edildikten sonra
   cevaplanır: bilinmeyen id **sıfır satır** okur — 28000'den ve 22023'ten ayrı bir cevap (Go'da
   `ErrNoSuchTenant`).
3. **Audit satırı ne okunduğunu söyler, operatörün ne yazdığını söylemez:** liste için
   `target_scope = 'tenants'`, sayfa ve `detail = {"search": <sınıf>}` (§2 v 1'in *"arama
   yapıldı"* olgusu; anahtar kümesi testte birebir); terim ne ham ne hash'i ile hiçbir satırda
   yoktur (adres biçimli bir terimle ölçüldü). **Sınıf (2. tur, orkestratör kararı — güvenlik
   denetiminin ORTA bulgusu):** tanımlayıcı onu terimden türetir, sırayla ilk uyan: `none`
   (boş terim) · `id` (terimin tamamı tireli bir uuid) · `address` (terim `@` içerir) · `text`
   (geri kalan). Kurallar `op_read_tenants`'ın koşabildiği dallardır (id dalı yalnız tam uuid'de,
   adres dalı yalnız `@`'li terimde — aynı yüklemle kapılı, md. 4), yani sınıf terimin
   ulaşabildiği en dar dalı adlandırır: `text` bir arama yalnız ad eşleştirebilir. Gerekçe:
   birebir adres araması *"bu adres hangi tenant'ın yöneticisi"* sorusunu cevaplar, ve yalnız
   `{"search": true}` iken bir adres listesini deneyen bir operatör ya da DSN sahibi ad
   aramalarından ayırt edilemeyen N arama olarak görünüyordu. **Sınıf içeriksizdir — terimin hiçbir
   karakterini taşımaz — ve kötüye kullanımın hacmini türüyle birlikte görünür kılar.** Çağıran
   sınıf hakkında yalan söyleyemez: sınıf terimden burada türetilir ve terim bilet hash'ine
   bağlıdır, yani ardından gelen okuma sınıfın türetildiği terimi koşar. Ayrıntı için `target_scope =
   'tenant_detail'`, `target_tenant_id` = istenen id, sayfa yok, `detail = {}`; bilet satırı da aynı
   `target_tenant_id`'yi taşır (bilgi amaçlı — bileti tenant'a bağlayan hash'tir). Yeni audit türü
   eklenmedi; kapalı küme yalnız bilet türlerinde büyüdü.
4. **Arama — tek terim, üç şey:** (a) ad, büyük/küçük harfe duyarsız **alt dizi** olarak,
   `strpos(lower(name), lower(term)) > 0` — **LIKE değil**: `%`, `_`, `\` kaçırılacak bir
   metakarakter değildir, kaçış da yoktur (ölçüldü: her biri yalnız onu içeren adı bulur; fikstürde
   kaçışsız ILIKE farklı cevap verir); (b) bir **yönetici adresi, BİREBİR** (citext,
   `OPERATOR(public.=)` — M6-01 tuzağı): alt dizi araması kişisel verinin taranması olurdu
   (*"@gmail"*), birebir eşleşme bir aramadır — operatör adresi zaten bilmelidir; her durum ve rol
   dahil; adres döndürülmez; `employees.email` aranmaz; eşleşme değerle bağlıdır (`t.id IN
   (yöneticilerin tenant_id'leri)`), yani adres yalnız KENDİ tenant'ını bulur; **adres dalı yalnız
   `@` içeren terimde koşar** (2. tur: sınıfın `address` yüklemiyle aynı — `@`'siz bir terim
   `text`'tir ve yalnız ad eşleştirir; kaybedilen, `@`'siz bir yönetici adresidir: kayıt onu
   reddeder, dev'de 0, ölçüldü); (c) tenant **id**'si, terim tam bir tireli uuid ise. **Başka hiçbir
   şey eşleşmez** — plan, işletme türü, yapı, saat dilimi, VAT numarası ve `@`'siz bir yönetici
   adresi terim olarak verildiğinde hiçbir fikstür dönmez ve dönen her satırın ADI terimi taşır
   (2. tur: üçüncü göz `OR t.plan = p_query` mutasyonunu yeşil ölçmüştü). Sıra `created_at DESC, id DESC`: `tappa_app` `name`'i
   güncelleyebilir, `created_at`'i güncelleyemez (00024), yani adını değiştiren bir tenant
   operatörün OFFSET sayfalarını kaydıramaz.
5. **Sütunlar (§2 ii):** liste `tenant_id, tenant_name, created_at, plan` — listede bir tenant'ı
   ötekinden ayıran şey, fazlası değil. Ayrıntı: aynı dört + `business_type` +
   `location_count`, `active_employee_count`, `active_plaque_count`, `active_admin_count`
   (statüsü `active` olanlar; lokasyonun statüsü yok). **`structure` YOK — ölçülerek çıkarıldı:**
   `tenants.structure`'ı kayıttan sonra hiçbir şey okumaz (M7-03 B kararı;
   `TestSignupStructure_DecidesNothingAfterSignUp` onu okuyan her Go seçicisini sayar ve ilk
   tasarımın `TenantOverview.Structure`'ını kırmızıyla yakaladı) — operatörün genel bakışı onun ilk
   okuyucusu olmaz. Ad, adres, saat, plaket uid'i, anahtar yok;
   statüye göre envanter OP-13'ün, faturalanan kişi sayısı OP-12'nin okumasıdır. Ayrıntının
   tenant verisine dokunan her sorgusu (satır ve dört sayım) tenant'ı adlandırır
   (`x.tenant_id = p_tenant_id`, `t.id = p_tenant_id`) — kuşağı olmayan kemer.
6. **Tanımlayıcının yeni yetkileri — tenant tablolarında İLK:** `tenants` SELECT (id, name,
   created_at, plan, business_type); `locations` SELECT (tenant_id); `employees` SELECT
   (tenant_id, status); `tags` SELECT (tenant_id, status); `admin_users` SELECT (tenant_id, email,
   status). §1'in "asla" sütunlarından hiçbiri yok; katalogdan türetilen sır biçimli dokuz
   tenant-tablosu sütununun (yedi "asla" + `tenant_branding.logo`, `logo_sha256`) hiçbirinde
   `has_column_privilege(tappa_opdefiner, …, 'SELECT')` doğru değil ve tanımlayıcı olarak
   üçünü (iki plaket anahtarı, yönetici digest'i) okuma denemesi 42501 — gövde o rolle koştuğu
   için değiştirilmiş bir gövde de onları döndüremez.
   `TestOperator00026_PrivilegeMatrix`'in izin listesi OP-5 notunun kuralıyla, tam sütun
   listesiyle aynı değişiklikte genişletildi. `tappa_app` hiçbir yetki almadı; `tappa_operator`
   iki yeni fonksiyonda EXECUTE aldı, hiçbir tablo yetkisi almadı.
7. **Down:** iki okumayı düşürür, `op_begin_read`'i 00027'nin gövdesine **birebir** döndürür
   (test 00027'nin dosyasıyla karşılaştırır), tanımlayıcının beş tablodaki bütün yetkilerini alır,
   bilet tür CHECK'ini 00027'nin kümesine döndürür — iki türden HERHANGİ birinden bilet VARSA
   `NOT VALID` (hiçbir ürün yolu bilet silmez; ekranlar canlıda kullanıldıktan sonra bu dal
   seçilir; test üç dalı ayrı sürer: yalnız `tenants` bileti, hiç bilet, yalnız `tenant_detail`
   bileti). Ölçüm (dev, `pg_dump --schema-only`, `\restrict` satırları ayıklanarak): v28 → Up →
   Down şeması v28'le birebir (sha256 öneki `f0dc03e20de806e8` iki tarafta), Down → Up v29
   birebir (2. turun dosyasıyla `24c6f40fcaebea22` iki tarafta).
8. **Maliyet — gözlem, hedef değil** (dev, 2026-10-03; 586 538 tenant — test kalıntısı, müşteri
   değil): liste paralel sıralı tarama + top-N sıralama, ilk sayfa terimli/terimsiz 88–196 ms;
   derin sayfa her şeyi sıralar (200'lük 2000. sayfa 580 ms, diske taşan birleştirme). İndeks
   eklenmedi: üretimde tenant sayısı onlarla ölçülür. Çalışanı en çok olan dev tenant'ın ayrıntısı
   (kadrosu test kalıntısı ve her koşuda büyür; boyu yazılmadı, sorgusu migration'ın yorumunda)
   sıcak 10–13 ms, soğuk 1,4 sn. (Tenant sayısının sorgusu: `SELECT count(*) FROM tenants`.)
9. **Başka görevlerin testlerinde zorunlu güncellemeler (zayıflatılmadı, gerekçeleriyle):**
   `TestOperatorSQL_OnlyBoundParameters` 10 → 12 sabit/çağrı; `TestOperatorAccessors_TheCustomerRoleCannotUseThem`
   iki yeni erişimci; `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`'in öncülü *"tam
   olarak 00027'nin kümesi"* yerine *"`legal_versions`'ı tutan kapalı bir küme"* (veritabanı HEAD'de);
   `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays` ve `TestOpBeginRead_RefusesDeadSessionsAndBadParameters`
   "küme dışı tür" kollarında artık üye olan `tenant_detail` / `tenants` yerine hiçbir migration'ın
   eklemediği bir ad; `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy` `locations`'taki sütun
   düzeyi ACL **sayısını** değil girişlerini okur — `tappa_app` için sıfır, öteki her giriş adıyla
   (bugün yalnız 00029'unki): 00010'un gerekçesi `tappa_app`'in tablo düzeyi yetkisi hakkındadır.
10. **Go tarafında aynı sınır (2. tur, güvenlik DÜŞÜK):** `TenantList` `MaxTenantSearchRunes`'tan
    uzun terimi gidiş-dönüşsüz `ErrTenantSearchRefused` ile reddeder; `op_begin_read`'in 254'ü
    ikinci kopyadır (TenantList olmayan çağıran için). Gerekçe: deyimlerin parametreleriyle
    log'landığı yerde (dev, `log_statement = all`) uzun terim, reddedilmek üzere tam hâliyle sunucu
    log'una gidiyordu.
11. **Sayılı sınırlar:** (a) **DSN sahibi (ya da oturumu olan bir operatör) bir adres listesini
    deneyebilir;** deneme başına commit edilmiş bir `read` satırı ve artık `address` sınıfı kalır,
    içerik kalmaz — hangi adreslerin denendiği izden okunamaz (bilinçli: §2 v 1), ne kadar ve hangi
    türde denendiği okunur; (b) sınıf, terimin ulaşabildiği dalı adlandırır, eşleşen dalı değil
    (`address` sınıflı bir arama yalnız adla da eşleşmiş olabilir); (c) `@`'siz bir yönetici adresi
    adres olarak aranamaz; (d) dev'de `log_statement = all` iken sınır içindeki terim de, her bağlı
    parametre gibi, sunucu log'una gider — güvenlik denetiminin uzun terim ölçümünden çıkarım, bu
    turda ölçülmedi (sunucu log'u okunmaz); yalnız yerel ("Karar verilmedi"nin aynı koşulu); (e) sınır 4'ün
    penceresi 30 sn; (f) liste maliyeti tenant sayısıyla doğrusal, derin OFFSET hepsini sıralar.

**Güvenlik iddiası — üç parça.**

- **PART I — bugün sevk edilen kodun ölçülen davranışı** (dev Postgres 17.10, 2026-10-03; ölçen
  testin adıyla):
  - `tappa_app` üç fonksiyonun hiçbirini çağıramaz (42501) ve `tenants` üzerindeki SELECT
    sütunları 00029'dan önceki gibidir; RLS onun için bozulmadı (iki tenant'tan uygulamanın
    bağlamında biri görünür, sahibin aynı ifadesi ikisini) · `tappa_operator` beş tablonun hiçbirinde
    yetki tutmaz, her birini doğrudan okuması 42501 · tanımlayıcı sır biçimli dokuz sütunun
    hiçbirini SELECT edemez (katalog), üçünü okuma denemesi 42501 — `TestOperator00029_TheDefinerReadsNamedColumnsAndNoSecret`.
  - Tam imzalar, sahip, tek overload, `proconfig`, PUBLIC/`tappa_app`/`tappa_resolver` için
    EXECUTE yok, ileri pin + donan saat + tüketim kaynak pini bulgusuz, genişletilmiş tür CHECK'i —
    `TestOperator00029_TheFunctionsAndTheirExactSignatures`.
  - Birinci aşamanın iki türü: tek `read` satırı ve tek bilet, satırın içeriği, saklanan hash'in Go'da
    yazılan kanonik metinle eşitliği (Go metni tırnak, ters bölü, kontrol karakteri ve ASCII dışı
    karakterde veritabanınınkiyle karşılaştırılır), terimin hiçbir yerde olmayışı; yirmi parametre
    reddi ve altı ölü oturum, satırsız; on bir terimde dört sınıf ve aralarındaki kenarlar (tek
    boşluk `text`, bir karakter fazlalı / süslü parantezli / tiresiz uuid `text`, `@`'li uuid
    `address`, tek `@` `address`) —
    `TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm`. 254 / 255 karakter (iki
    baytlık harfle) veritabanında; erişimcide 255 ve Go tarafı ret (UTF-8 dışı, NUL)
    gidiş-dönüşsüz, 254 gönderilir — `TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL`.
  - Arama: ad (Malta büyük harfi dahil), birebir adres (büyük harfli dahil; parçası ve çalışan adresi
    bulmaz; adres yalnız kendi tenant'ını bulur), id; boş terim fikstürleri ilk sayfaya koyar;
    plan, işletme türü, yapı, saat dilimi, VAT numarası ve `@`'siz adres hiçbir fikstürü bulmaz
    (kontrol: aynı adres `@`'li bulunur); id süslü parantezde ya da tiresiz bulunmaz (yalnız `id`
    sınıfının biçimi); tek boşluk ad eşleşmesidir, bütün liste değil; okuma aşaması audit satırı yazmaz —
    `TestOpReadTenants_SearchMatchesNameAddressAndIDOnly`. `%`, `_`, `\`
    — `TestOpReadTenants_LikeMetacharactersAreLiteral`. 200 tavanı, sıra ve id eşitlik kırıcısı, OFFSET —
    `TestOpReadTenants_PagesAreCappedAndOrdered`. İki tenant'lı kemer ve bilinmeyen tenant'ın sıfır
    satırı / `ErrNoSuchTenant` — `TestOpReadTenantDetail_CountsOnlyTheNamedTenantsRows`.
  - İki okuma için: bu transaction'ın (üst düzey, açık ve bırakılmış savepoint) bileti 28000 —
    `TestOpReadTenants_ATicketFromThisTransactionIsRefused`; başka oturum, terim, sayfa, tenant,
    hiç verilmemiş ve NULL bilet, ve **tür karışıklığı** (okumanın kendi hash'i öteki okumanın ya da
    `legal_versions`'ın türüyle) 28000 — `TestOpReadTenants_AForgedTicketIsRefused`; altı ölü oturum —
    `TestOpReadTenants_RefusesEveryDeadSession`; süre (dolmuş; açık transaction'da savepoint ×3 ve
    uykusu içinde tek `DO`, 0/6) — `TestOpReadTenants_ExpiryIsTheWallClock`.
  - Gerçek commit'lerle: okuma başına tam 1 `read` satırı (iki tür için ayrı ayrı), geri alınan
    okumada satır kalıcı ve bilet yeniden okunur (sınır 4), commit edilmiş tüketimden sonra 28000 —
    `TestOpReadTenants_TwoPhaseLifecycle`. Üretim kurucusunun havuzunda iki aşama iki transaction,
    commit edilen satırın sayfası ve `{"search": "text"}`'i, terimsizliği, bilinmeyen tenant'ın
    commit edilmiş satırı + `ErrNoSuchTenant`, tek transaction'da ve bilinmeyen oturumda
    `ErrOperatorRefused`, 201'lik sayfa 22023 ve satırsız, büyük harf ve Malta harfli bir terim iki
    aşamadan geçer, üç ret (oturum, sayfa, sınır) hata metninde terimi taşımaz —
    `TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions`.
  - Down/Up ve üç dalı — `TestOperator00029_DownRestoresTheLegalOnlyReadAndUpTakesItAgain`; ön koşulun
    dokuz rol şekli — `TestOperator00029_PreconditionRefusesAWrongCluster`; dokuz tablo adının
    çağıranın geçici tablosuyla gölgelenmesi — `TestOperator00029_CallersTempTableIsNeverRead`.
- **PART II — adıyla pinler ve yakaladıklarının tam listesi:** `TestOperator00029_TheFunctionsAndTheirExactSignatures`
  — üç fonksiyonun tam argüman listesi ve sonuç tipi (dönüş kümesine bir sütun giremez), sahip,
  ad başına tek overload, `proconfig`, üç rol için EXECUTE yokluğu, ileri pin, donan saat ve
  tüketim kaynak pininin bu üçüne dair bulguları, bilet tür CHECK'inin tanımı;
  `TestOperator00026_PrivilegeMatrix` (genişletildi) — tanımlayıcının tenant tablolarındaki tam
  sütun listeleri; `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays` — adı `op_read_` ile başlayan
  her fonksiyon (bu ikisi dahil, testte adıyla doğrulanır); `TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource`
  — değiştirilen `op_begin_read`'de biletin kaynağı; `TestOperatorSQL_OnlyBoundParameters` — on iki sabit
  ve çağrıları; `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy` — `locations`'ın sütun düzeyi ACL
  girişleri; `TestSignupStructure_DecidesNothingAfterSignUp` — kayıt yolu dışında `Structure` adlı
  bir Go seçicisi (genel bakışın tipi dahil). Mutasyon tablosu OP-11A kart düzeltmesinde.
- **PART III:** listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**OP-11 B fazı eki (2026-10-03, ekranlar ve wiring).** Yukarıdaki notun *"ekran, wiring ve
`*OperatorDB` yöntemleri B fazıdır"* cümlesi A fazının kaydıdır; B fazında tüketici yazıldı:
`internal/handler/operator/tenants.go` (tüketici arayüzü `TenantStore`, üç handler, sınır
denetimleri), `web/templates/operatorpages/tenants.templ` (`Tenants`, `TenantOverview`),
`*OperatorDB`'ye `TenantList(ctx, sessionHash, q)` ve `TenantDetail(ctx, sessionHash, id)` —
ikisi de `return F(ctx, o.pool, …)`. Migration yok; bağımlılık yok. Kararlar, ölçümüyle:

1. **Rotalar konsolun grubunda:** `GET /operator/tenants` (her tenant'ın ilk sayfası), `POST
   /operator/tenants` (arama ve her sayfa), `GET /operator/tenants/{id}` (genel bakış) — host
   kapısı → güvenlik başlıkları → flood → same-origin (okuma kapısıyla) → `requireOperator` →
   `sessionGate`. Konsol (`Home`) `/operator/tenants`'a link verir; `/operator`'un kendisi liste
   DEĞİL: liste bir okumadır — iki bütçe birimi ve bir audit satırı — ve her girişin indiği
   sayfa bu bedeli taşımaz. (Orkestratör kararı K2, 2026-10-03: konsol menü kalır; ADR 0020
   §4'ün *"Rotalar"* maddesi ve §9'un *"Ekranda"* hücresi buna göre düzeltildi.)
2. **Arama terimi isteğin yalnız gövdesinde; URL'de, `Location`'da, süreç log'unda ve audit
   satırında yok (ölçülen kollar, PART I)** (A notu md. 14.3'ün (a) seçeneği). Tasarım gereği
   gittiği yerler: arandığı sonuç sayfası, `internal/db`'nin iki aşamaya bağlı parametresi ve
   deyimlerini parametreleriyle log'layan bir veritabanının kendi sunucu log'u (dev'in
   `log_statement = all`'ı; üretim deyim log'lamaz — OP-11 notu md. 11 (d)). `GET
   /operator/tenants` URL'den hiçbir şey okumaz (`?q=`, `?page=` arama değildir); her sayfa —
   boş terimli olanlar da — bir POST'tur, sayfalayıcı terimi ve sayfayı gizli alanlarda taşır.
   **PRG yok:** yönlendirmenin GET'i terimi bir URL'de, bir çerezde ya da sunucuda durum olarak
   taşımak zorunda kalırdı; formu yeniden gönderen bir yeniden yükleme (tarayıcı önce sorar)
   bir okuma daha olur (audit'te bir satır daha — doğru kayıt). Terim sayfada gösterilir — arama
   kutusunda, *"matching"* satırında, sayfalayıcının gizli alanlarında — templ'in kaçışıyla ve
   `bdi` ile yalıtılmış; ret, hata ve yönlendirme yanıtlarında yoktur; kutu `autocomplete="off"`.
   Terim formdan çıkışına dek `formValue`'dadır (yer tutucuyu basar); `reveal()` üç yerde: sınır
   denetimi, `db.TenantListQuery.Search`, `operatorpages.TenantsView.Search`.
3. **Sınır (CLAUDE.md §7):** terimin uçları kırpılır; kalan geçerli UTF-8, en çok
   `db.MaxTenantSearchRunes` karakter, kontrol karakteri (C0, C1) ve satır/paragraf ayırıcısı
   yok — yoksa 400, store çağrısı yok. Sayfa 1..1000 (`maxTenantPage`: OFFSET maliyeti A notu
   md. 8; 1 000 × 50 = 50 000 tenant), yalnız ondalık rakam; sayfa boyu 50 (`tenantPageSize`,
   sabit — formdaki bir `size` okunmaz). Sonraki sayfa yalnız TAM sayfadan sonra ve 1000'den önce.
   Id yalnız 36 karakterlik tireli biçim (iki harf büyüklüğü); başka biçim 404 (*"That link does
   not name a tenant"*), store çağrısı ve ikinci bütçe birimi yok (2. turdan beri ölçülü: 99 bozuk
   id 99 birim). `ErrNoSuchTenant` → 404, ayrı sayfa; `ErrOperatorRefused` → oturum
   açmanın 303'ü (legal ekranıyla aynı: çerez burada silinmez); başka her veritabanı hatası →
   503 (A notu md. 14.4'ün *"22023 → 500"* önerisi yerine: `internal/db`'nin hatası SQLSTATE'i
   yalnız metninde taşır ve handler'ın doğruladığı yerde 22023 ulaşılamazdır; log satırı
   SQLSTATE'i taşır).
4. **Adsız tenant (A notu md. 14.5 seçimi B'ye bıraktı):** `tenants.name`'de boş olmama CHECK'i
   yok; görünür karakteri olmayan bir ad (legal ekranının `visibleText` kuralı) başlıkta
   `operatorpages.UnnamedTenant(id)` ile — *"Unnamed tenant"* ve altında id, belge başlığında
   *"Unnamed tenant <id>"* — liste satırında *"Unnamed tenant"* olarak çizilir. 500 seçilseydi o
   tenant'ın genel bakışı hiç açılamazdı; tenant yine id'siyle adlandırılmış olur. Sıfır
   `TenantName` hâlâ reddedilir (`TenantScreen` → render'ın 500'ü).
5. **Bütçe yeniden türetildi: `sessionLimit` 100 → 200** (orkestratör kararı K1, 2026-10-03:
   200 kalır). Her liste sayfası, arama sayfası ve genel bakış iki birimdir (`sessionGate` +
   handler'ın `spendSession`'ı); bozuk id'li bir genel bakış bir birim. Tarayıcının geri tuşu bir
   sonuç sayfasına dönerken `no-store` bir POST yanıtına döner; tarayıcı aramayı yeniden
   gönderirse (önce sorar; ölçülmedi) bir okuma daha olur, yani sonuç listesinden açılan bir
   tenant iki okumaya mal olabilir. Bir operatör × (~15 genel bakış × 2 + ~15 liste ya da arama
   sayfası × 2 + ~5 legal görüntü × 2 + ~5 konsol + birkaç yayın) ≈ 75 / pencere, × ~2,7 pay →
   200. **Bu bir tahmindir, kullanım ölçümü değil:** operatör yüzeyinin kullanım verisi yok.
   **Bedeli:** çalınmış bir oturum çerezi pencere başına 100 okuma — en çok 5 000 liste satırı
   (50'lik 100 sayfa) — yaptırabilir ve her okuma `operator_audit_log`'da bir `read` satırı
   bırakır. Bir okuma isteği ÜÇ tanımlayıcı işlemidir — `sessionGate`'in `op_touch_session`'ı,
   `op_begin_read`, `op_read_*` (sızıntı testinin hasadı üçünü de sayar) —, yani bütçenin KABUL
   ettiği istekler için tavan 100 okuma = 300 tanımlayıcı işlemi + 100 `read` satırı, ya da 200
   konsol görüntüsü = 200 yüklem çağrısı (3. tur, güvenlik F2; 2. turdaki "200 işlem" yanlıştı).
   Bütçenin REDDETTİĞİ istek yüklemi zaten koşmuştur (`Verify`, `spendSession`'dan önce): yüklemin
   kendi işini bu sayı değil, adres başına flood kapısı sınırlar. **Daha dar biçim, devir (OP-13 B):** oturuma bağlı ayrı bir OKUMA sınırlayıcısı
   (ör. `readLimit` 60 / 10 dk) ve `sessionLimit` 100'de; gerekçe: OP-12/13/14'ün okumaları bu
   türetmeyi ≈280'e taşır (planlayıcının tahmini).
   *(OP-13 notu, 2026-10-06: bu madde OP-11 B'nin bütçesini kaydeder ve o gün için doğrudur.
   OP-13 B'den itibaren yukarıdaki "daha dar biçim" yapıldı: bir okuma oturum bütçesine
   (`sessionLimit` 100, yalnız istek sayar) `sessionGate`'te bir birim ve ayrı bir okuma
   bütçesine (`readLimit` 60 / 10 dk, `spendRead`) handler'da bir birim sayılır; okumanın ikinci
   birimi artık oturum bütçesinin değildir ve `sessionLimit` 200 değil 100'dür. Aynısı bu ekin
   md. 1'indeki *"iki bütçe birimi"*, md. 3'ündeki *"ikinci bütçe birimi"* (bozuk id bugün okuma
   birimi harcamaz) ve aşağıdaki güvenlik iddiasının *"ikinci bütçe birimi"* için de geçerlidir.
   Bkz. "OP-13 B fazı eki" md. 8.)*
6. **Wiring:** `cmd/tappa`'nın `operatorStore`'u `operatorauth.Store` ∪ `LegalStore` ∪
   `TenantStore`; `configuredSurface`'in store'u TAM üç kullanım (`arg0 of operatorAuthenticator`,
   `arg1 of operator.New`, `arg2 of operator.New`), `texts` `arg3 of operator.New`.
   `legalSession` iki ekranın ortak yardımcısı olarak `storeSession` adıyla `routes.go`'ya taşındı
   (davranış aynı).

**Güvenlik iddiası — üç parça.**

- **Tehdit modeli:** Bu ölçümler ve pinler, tenant ekranlarının koduna kazara giren bir
  değişikliğe karşıdır — terimi bir log satırına, bir URL'e ya da başka bir sayfaya taşıyan, bir
  sınır denetimini ya da ikinci bütçe birimini düşüren, bir adı kaçışsız çizen bir düzenleme —
  ve bir oturum sahibinin URL, başlık ve form üzerinden yapabildiklerine (ölçülen kollar). Paketin
  sınırlarını bilerek atlatmak için yazılmış kod (başka bir paketten log, bir `ResponseWriter`
  sarmalayıcısı, yansıma) ve süreç dışındaki yüzeyler (ingress log'u, tarayıcı, PostgreSQL'in
  deyim log'u) kod incelemesinin ve sayılı sınırların konusudur.
- **PART I — bugün sevk edilen kodun ölçülen davranışı** (2026-10-03; ölçen testin adıyla):
  - Terimin yolu, GERÇEK bir sunucudan telden okunarak: iki terimin (ad ve adres biçimli) kendisi,
    ilk sekiz ve ilk dört karakteri, altı yazımda (ham, sorgu-kaçışlı, yol-kaçışlı, HTML-kaçışlı,
    `%q`, JSON) on kolda süreç ve erişim log'unda ve telin başlıklarında YOK; gövdede yalnız iki
    sonuç sayfasında VAR (pozitif kontrol: kutu, *"matching"* satırı, gizli alan) ve orada da
    hiçbir `href`/`action`/`src` değerinde yok; o sayfada terim ve ilk dört karakteri tam üçer kez
    (kutu, satır, Next'in gizli alanı) ve belge başlığında (`<title>`, tam olarak *"Tenants —
    Taptime operator"*) hiç yok (3. tur, güvenlik F1); oturum reddinin
    `Location`'ı tam olarak `/operator/login`; URL'deki terim store'a `""` ve sayfa 1 olarak gider —
    `TestTenantSearch_NoLogLineHeaderOrOtherPageCarriesTheTerm`. Sızıntı sözleşmesinde G17 (terimler
    ve önekleri), A44–A59 on altı kol, D7 (sonuç sayfası) ve hasat (`TenantList` 7×2,
    `TenantDetail` 4×1) — `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`.
  - Sınır: on iki terim ve on bir sayfa reddi 400 ve SIFIR `TenantList` çağrısı (internal/db'nin
    reddettiği çağrı da sayılır); kabul edilenler kırpılmış terimle; 16 KiB gövde 413; ret sayfaları
    ve kutunun `maxlength`'i `db.MaxTenantSearchRunes`'tan — `TestTenantSearch_TheBoundaryRefusesBeforeTheStore`.
  - Yirmi yedi yanıt sınıfı (C67–C93) düşmanca istek başlıkları ve URL'de terim ve sayfayla:
    tasarlanan başlıklar, oturum açmanınki dışında `Location` yok, yansıma yok; sınıf başına store
    sayıları — `TestOperatorHeaders_TheTenantClassesCarryThePolicy`.
  - Bütçe: 100 genel bakış 200, 101. kapıda 429; üç okumanın her biri 199 birimden sonra ikinci
    biriminde 429, store çağrısız; 1 konsol + 99 bozuk id'li genel bakıştan sonra 100 konsol
    görüntüsü 200, 101.si 429 (bozuk id bir birim) — `TestTenantPages_AReadCountsTwiceAgainstTheSessionBudget`;
    `TestSessionGate_ABudgetPerSession` (201. konsol isteği 429),
    `TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget` (100 legal görüntü).
  - Ad ve id: başlıkta ve belge başlığında kaçışlı ad, etiketine bağlı mono olgular ve sayımlar,
    UTC+2'de saklı kayıt zamanı genel bakışta ve liste satırında UTC, yedi bozuk yol biçimi 404 ve
    store'suz, bilinmeyen id 404, hata 503 ve log satırında id var oturum hash'i yok —
    `TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath`; altı görünmez ad yer
    tutucuyla, liste satırında id'si görünür metin olarak — `TestTenantOverview_AnUnnamedTenantIsNamedByItsID`;
    kaçış ve `bdi` — `TestTenantScreens_EscapeWhatATenantAndAnOperatorTyped`; sayfalayıcı ve
    aranmamış listede *"matching"* satırının yokluğu — `TestTenantList_PagesForwardOnlyAfterAFullPage`;
    kontrast (palet; şablonların mürekkep tonları `TestBrand_EveryInkToneClearsAA`'da) —
    `TestTenantScreens_TheTextClearsAA`.
  - PostgreSQL'e karşı uçtan uca: liste, üç arama (`id`/`text`/`address` sınıfları), genel bakış
    (owner'ın sayımlarıyla eşit), bilinmeyen id (adını taşıyan `read` satırı), bozuk id (satırsız),
    üç ölü oturum (303, satırsız); kelime ve adres operatörün hiçbir audit satırında, bilet
    satırında ve log'da yok; her okumanın bileti tüketilmiş —
    `TestE2E_TenantScreensReadThroughTheDefinersAndAuditEachRead`. Yöntemlerin kendisi üretim
    kurucusunun havuzunda iki işlem — `TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions`.
- **PART II — adıyla pinler ve yakaladıklarının tam listesi:**
  `TestOperatorDB_IsTheStoreAndNothingMore` — `*OperatorDB`'nin yöntem kümesi `operatorauth.Store`
  ∪ `LegalStore` ∪ `TenantStore` ∪ `Close` (üç arayüz ortak ad taşırsa kırmızı);
  `TestOperatorDB_EveryMethodDelegatesVerbatim` — on bir yöntem, argümanlar sırasıyla;
  `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` — öncül 12; `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator`
  — store'un üç kullanımı, `texts`'in dördüncü argüman oluşu; `TestFormValues_TheListedSitesAloneRevealOrReadTheForm`
  — FV1 V4–V6 (`reveal()`'ın üç yeni yeri), FV3 `"q"` (yalnız `postValue`'nun adı olarak), FV5
  `searchTenants`'ın `.Get("page")`'i bir kez; `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass`
  — sekiz rota, on üç çift, C1–C93; `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders`
  — dokuz ekran kurucusu; `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` — yirmi
  değişken; `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` — yirmi bir render, `{id}`
  rotası adıyla; `TestCustomerPanel_EverySectionCarriesNoOperatorElement` (değişmedi) — müşteri
  panelinde `/operator` işareti. Mutasyon tablosu ve sayılı sınırlar (LT1–LT14: tarayıcının POST
  geçmişi, ingress'in gövde log'u, geri tuşunun yeniden gönderimi, dev'in deyim log'u, 1000 sayfa,
  …) OP-11B kart düzeltmesinde.
- **PART III:** listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

## OP-13 uygulama notu (2026-10-03, A fazı — veri katmanı)

Uygulama: `db/migrations/00030_read_plaques_from_the_operator.sql` + `internal/db/operator.go`
(dışa açık `TenantPlaques`, `TenantPlaqueInventory` (+`Truncated`), `TenantPlaque` (+`Shape`),
`PlaqueShape` ve yedi değeri, `MaxTenantPlaques`; paket içi `readTenantPlaques`) +
`db/queries/operator.sql` (belge) + `internal/db/operatorplaques_test.go`. Ekran, wiring ve
`*OperatorDB` yöntemi B fazıdır. Kararın gövdesi değişmedi; uygulamanın karar verdiği yerler,
adıyla:

1. **Ad §2 v 6'nın kuralıyla:** `op_read_tenant_plaques(p_session, p_ticket, p_tenant_id uuid)`.
2. **`op_begin_read` yerinde değiştirildi** (`CREATE OR REPLACE`): 00029'un Up gövdesi + iki
   satır — kapalı küme `tenant_plaques`'ı adlandırır ve bu tür 00029'un `tenant_detail` dalını
   alır (`{tenant_id}`, tireli uuid, iki harf büyüklüğü; hash'lenen metin uuid değerinden). İki
   tür aynı tenant için **aynı** metni hash'ler; bu yüzden bir genel bakış biletini envantere
   (ve tersini) açtırmayan şey biletin **türü**dür — iki okumanın da tüketen `UPDATE`'indeki
   `k.kind = '…'` koşulu burada yük taşır (OP-11'de iki okumanın parametre nesneleri
   farklıydı). Birinci aşama tenant'a yine **bakmaz** (B14'ün okuma hâli).
3. **Audit satırı:** `target_scope = 'tenant_plaques'`, `target_tenant_id` = istenen id, sayfa
   yok, `detail = {}`; bilet satırı aynı `target_tenant_id`'yi taşır. Yeni audit türü yok.
4. **Tek okuma, üç cevap** (ekranın bütçesi bir okuma, iz bir satır): başlığın **adı** her
   satırda; **varlık** — bilinmeyen id **sıfır satır** (Go'da `ErrNoSuchTenant`, okuma satırı
   zaten commit edilmiş), 28000'den ve 22023'ten ayrı; **plaketler** — plaketsiz tenant plaket
   sütunları `NULL`, `plaque_count` 0 olan **tek** satır okur (`LEFT JOIN`).
5. **Sütunlar (§2 ii):** tenant'ın kendi listesinin (`ListTagsForTenant`) sütunları + girişin
   adı: `tenant_id, tenant_name, uid, status, location_id, location_name, encoded_at,
   created_at, retired_at, replaced_by, last_ctr, plaque_count`. **`last_ctr` döner**
   (orkestratör kararı K13-1): sayaç bir sır değildir (plain SDM'de URL'de düz, ADR 0003), §4.7
   yalnız anahtarları korur, ve *"plaketim çalışmıyor"* sorusunun ilk cevabı odur. **Anahtar
   yok** — ne sütun olarak ne bir ifade olarak; *"anahtar 0 var mı"* da bir anahtar okumasıdır
   ve döndürülmez (iki plaket yalnız `app_key_ref`'te ayrışırken okuma onları uid dışında
   **aynı** satır olarak verir — ölçüldü). Tek encode sinyali `encoded_at`'tir.
6. **Durum olduğu gibi döner, Go'da kapalı okunur.** SQL `status`'u eşlemez, süzmez
   (`CASE`/`WHERE` yok): şemanın bugün adlandırmadığı bir değer çağırana kendisi olarak ulaşır.
   `TenantPlaque.Shape()` `status × encoded_at × konum` üzerinden **kapalı** bir eşlemedir: altı
   şekil şemanın izin verdiği hâllerdir — **A-1** (`active`, duvarda, damgasız; backlog T75)
   kendi şeklidir (`on_a_wall_never_encoded`) —, geri kalan her şey (beşinci bir değer, boş,
   başka harf büyüklüğü, şemanın yasakladığı bir kombinasyon) `unrecognised`'dır: satır
   düşürülmez, komşu bir duruma okunmaz.
7. **Sıra, tavan, sayı:** tenant'ın kendi listesinin sırası (`location_id NULLS FIRST, uid` —
   uid birincil anahtar, sıra tam); gövdede `LIMIT 200`; `plaque_count` pencereyle LIMIT'ten
   **önce** sayılır (`count(g.uid) OVER ()`), yani *"first 200 of N"* söylenebilir. Sayfalama
   **yok** (K13-2); platform geneli stok görünümü **yok** (K13-3, OP-18 tasarlanınca ölçülür);
   T76 bu karta **katılmadı** (K13-4).
8. **Kemer — her tablo referansı tenant'ı adlandırır** (§3.2): `t.id = p_tenant_id`,
   `g.tenant_id = p_tenant_id` (JOIN koşulunun kendisinde, `t` üzerinden değil),
   `l.tenant_id = p_tenant_id`. Üçüncüsü bugün **yapısal olarak gereksizdir**:
   `tags_location_fk` = `(location_id, tenant_id) → locations (id, tenant_id)`, yani bir
   plaketin girişi daima kendi tenant'ınındır; filtre kural gereği yazıldı (sınır L1).
   `replaced_by` yalnız metin olarak döner, birleştirilmez; `tags_replaced_by_fk` de bileşiktir
   (`(replaced_by, tenant_id) → tags (uid, tenant_id)`), başka tenant'ın uid'ini adlandıramaz
   (ölçüldü: 23503).
9. **Tanımlayıcının yeni yetkileri — yalnız yeni sütunlar:** `tags` SELECT (uid, location_id,
   last_ctr, retired_at, replaced_by, created_at, encoded_at) — 00029'un `tenant_id, status`'u
   ile iki anahtar dışındaki **her** sütun —; `locations` SELECT (id, name). 00029'un
   sütunları **yeniden verilmedi** ve Down onları **almaz**: sütun başına alıcı başına tek ACL
   girdisi vardır, `REVOKE ALL` (00029'un Down'ının yazımı) genel bakışın sayımlarını 42501'e
   çevirirdi. Down sütun düzeyinde geri alır; ölçüldü: Down sonrası tanımlayıcının listeleri
   00029'unkiler, genel bakış hâlâ okur. `tappa_app` hiçbir yetki almadı; `tappa_operator`
   yalnız yeni fonksiyonda EXECUTE aldı, hiçbir tablo yetkisi almadı.
10. **§4.4 — okuma `tags`'e yazmaz:** tanımlayıcının `tags` üzerinde INSERT/UPDATE/DELETE/
    TRUNCATE'i yok; okumadan sonra her fikstür satırının fiziksel sürümü (`ctid`, `xmin`) ve
    `last_ctr`'ı aynı; ardından sevk edilen `AdvanceTagCounter` sayacı ilerletir, aynı değerin
    tekrarı `pgx.ErrNoRows`'dur, ikinci okuma ilerlemiş değeri verir.
11. **Yeni pin — tanımlayıcının `op_*` dışı EXECUTE'u** (OP-12 planının notu): sahibi
    olmadığı ve EXECUTE edebildiği her fonksiyonu PUBLIC de edebilir ve hiçbiri SECURITY
    DEFINER değildir — bütün şemalarda. Adıyla: `resolve_tag_by_uid` (`tappa_resolver`'ın,
    dönüşünde `aes_key_ref` var) tanımlayıcıya kapalı. 00030 bu kümeye bir şey eklemez.
    OP-12'nin fatura yardımcılarına vereceği EXECUTE bu pine adlı bir izin listesiyle girer.
12. **Başka görevlerin testlerinde güncellemeler (zayıflatılmadı):**
    `TestOperatorSQL_OnlyBoundParameters` 12 → 13 sabit/çağrı;
    `TestOperatorAccessors_TheCustomerRoleCannotUseThem` + `TenantPlaques`;
    `TestOperator00026_PrivilegeMatrix` izin listesi (`tags`, `locations` tam sütun listeleriyle);
    `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy` adlı ACL listesi (+`id`, `name`; `wifi_ssid`
    yine yok); `TestOperator00029_TheFunctionsAndTheirExactSignatures`'ın tür CHECK pini *"00029'un
    üç türünü tutan kapalı küme"* oldu (HEAD'deki tam küme `TestOperator00030_TheFunctionAndItsExactSignature`'da);
    `TestOperator00029_DownRestoresTheLegalOnlyReadAndUpTakesItAgain` 00029'a önce sonraki her
    migration'ın Down'unu işlem içinde koşarak iner (00030'unkini) — yoksa öncülü HEAD'in
    dört türlü CHECK'ine takılırdı.
13. **Down/Up ölçümü** (dev, `pg_dump --schema-only`, `\restrict` satırları ayıklanarak): v29
    `24c6f40fcaebea22` → Up → `66338d9974f75bb3` → Down → `24c6f40fcaebea22` birebir → Up →
    `66338d9974f75bb3` birebir. Down'ın `op_begin_read` gövdesi canlı 00029 gövdesiyle bayt
    bayt eşit (uygulamadan önce salt-okumayla ölçüldü). Mutasyon döngülerinden sonra şema yine
    `66338d9974f75bb3`.
14. **Maliyet — gözlem:** plaketler `tags_tenant_idx`'ten, girişler `locations_tenant_idx`'ten,
    tenant'ın satırlarının tek sıralaması (gövdenin SELECT'inin en çok plaketli dev tenant'ında
    EXPLAIN ANALYZE'ı; o tenant'ın sorgusu migration'ın yorumunda).
15. **Down'ın tür koşulu (2. tur, üçüncü gözün bulgusu; Up değişmedi):** CHECK, 00029'un
    kümesinin **dışındaki herhangi bir** türden bilet varsa — tüketilmiş ya da değil, bu
    dosyanın türü ya da sonraki bir migration'ınki — `NOT VALID` döner:
    `WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail'])`. 1. turdaki
    `WHERE kind = 'tenant_plaques'` Down'ları **bileştirmiyordu**: Down'ı kendi türünün
    biletlerini `NOT VALID` bir CHECK altında bırakan sonraki bir migration, bu Down'a hiç
    adlandırmadığı bir tür bırakır ve doğrulanmış `ADD` 23514'le düşerdi (ölçüldü; şimdi
    testte bir dal). Küme 00029'un dosyasından türetilir ve test Down'ın üç kullanımını ona
    karşı pinler. **Sonraki A'lar için kural:** her yeni A'nın Down'ı, **önceki** migration'ın
    bildiği tür kümesinin **dışındaki her** bilette `NOT VALID` döner (kendi türünde değil);
    ve Up'ı da aynı soruyu sorar — kendi kümesinin dışındaki bir türden bilet varsa (daha
    sonraki bir migration'ın Down'ının bıraktığı) CHECK'i `NOT VALID` ekler, yoksa yeniden
    yukarı çıkış 23514'le düşer (3. tur, F2; 00030'un kendi Up'ı bunu yapmaz — L11).

**Sayılı sınırlar (OP-13 A):**
- **L1** — `l.tenant_id = p_tenant_id` filtresinin kaldırılması **YEŞİL** kalır (mutasyon U3,
  ölçüldü): bileşik `tags_location_fk` aynı tenant'ı yapısal olarak zorlar. Filtre bugün
  ölçülebilir bir etki taşımaz; FK düşerse kemer odur.
- **L2** — Go'daki satır-tenant denetimi (`errPlaqueOfAnotherTenant`) tek başına **ulaşılamaz**
  bir daldır (mutasyon G11 YEŞİL): SQL'in `t.id = p_tenant_id`'si önce cevap verir.
- **L3** — sınır 4'ün penceresi 30 sn (geri alınan okuma bileti yeniden okunur).
- **L4** — `search_path`'e `public` eklemek gölge testinde görünmez (gövde nitelenmiş); onu
  imza testi ve ileri pin tutar (mutasyon U15). Çağıranın `pg_temp`'te tanımladığı bir
  fonksiyon (örn. `pg_temp.sha256`) gövdenin çağrısını ele geçirmez — fonksiyon araması
  `pg_temp`'e bakmaz; ölçüldü, bir kez.
- **L5** — bir görünüm üzerinden anahtar okumak, tanımlayıcıya o görünümde bir yetki ister
  (ölçüldü: yetkisiz 42501); `public`'teki görünümlerdeki her yetki `TestOperator00026_PrivilegeMatrix`'in
  izin listesi dışında kırmızıdır, `pg_temp`'teki bir görünümü o tarama görmez.
- **L6** — havuz testinin işlenmiş-tenant yarısı veritabanının **en çok plaketli** tenant'ını
  okur ve yalnız değişmeyen olguları karşılaştırır (ad, uid'lerin o tenant'a ait oluşu, sıra,
  Total ≥ satır); veritabanında hiç plaket yoksa o yarı SKIP'tir (yalnız migrate'li veritabanı).
- **L7** — testler `tags`'e satır commit etmez (fikstürler geri alınan işlemlerde; ölçüldü:
  `op13` adlı tenant'ların plaketi koşudan önce ve sonra 0). Commit eden iki test koşu başına
  şunları bırakır (2. tur, ölçüldü): **+3** `read` satırı (yaşam döngüsü 1, havuz testi 2;
  append-only), **+2** `platform_admins` satırı (`disabled`) ve **+2** `platform_sessions`
  satırı (iptal edilmiş) — audit satırlarının yabancı anahtarları onları tutar; bilet 0.
- **L8** — tanımlayıcının sütun yetkisi tablonun gelecekteki sütunlarını kapsamaz; `tags`'e
  eklenecek yeni bir anahtar biçimli sütun türetilen listeye girer ve isimli denetimi kırmızıya
  çevirir (testin kendi kuralı) — bu bir tasarım, tamlık iddiası değil.
- **L9** — **30 → 29 → 28 zinciri, 00029'un kümesi dışında herhangi bir türden bilet varken
  VE 00029'un kendi iki türünden (`tenants`, `tenant_detail`) hiç bilet yokken 29 → 28'de
  düşer** (ölçüldü, testte iki dal: bir `tenant_plaques` bileti ve sonraki bir migration'ın
  türü; 3. tur, F2: 2. tur yalnız `tenant_plaques`'ı sayıyordu — 4. tur, N2: 3. turun
  "herhangi" hükmü bu ikinci koşulu söylemiyordu): 00030'un Down'ı `NOT VALID` döner; 00029'un
  Down'ı (uygulanmış, değiştirilemez) `NOT VALID` dalını yalnız kendi iki türünden bir bilet
  varsa seçer, yoksa doğrulanmış `ADD`'i 23514'le düşer. **Kendi türlerinden bir bilet varsa**
  — tenant ekranlarına hizmet vermiş bir veritabanının şekli — 00029'un Down'ı `NOT VALID`
  dalına gider ve 29 → 28 **geçer** (kapanış denetimi ölçtü; bu testin sürdüğü bir dal değil,
  00029'un Down kaynağıyla tutarlı). Yanılgı muhafazakâr yöndeydi. Yalnız ikinci koşulda,
  29'dan aşağı inecek biri önce kümenin dışındaki türlerin biletlerini kaldırmak zorundadır;
  bu dosya hiçbir bileti silmez.
- **L10** — Down ve ön koşul mutasyonları goose döngüsüyle değil, testlerin dosyanın kendi
  bölümlerini koştuğu geri alınan işlemlerde ölçüldü (bozuk bir Down'ı goose ile uygulamak
  paylaşılan veritabanını bozuk bir 29'da bırakırdı).
- **L11** — **Up tarafı da bileşmez** (3. tur, F2; ölçüldü, testte bir dal): sonraki bir
  migration'ın Down'ı kendi türünün biletini bırakmışken (ve 00030'un Down'ı koştuktan sonra)
  00030'un Up'ı yeniden koşarsa, §1'i dört türlü CHECK'i **doğrulanmış** ekler ve 23514'le
  düşer. Goose adımı kendi işleminde koşar: adım geri alınır, veritabanı 29'da ve bozulmamış
  kalır; yeniden yukarı çıkmak o biletlerin kaldırılmasını ister. 00030'un Up'ı bu turda
  değiştirilmedi (sevk kararı); kural aşağıda sonraki A'lar için.
- **L12** — **Gh pininin "her dosya"sı testin derleme bağlamıdır** (4. tur, N3):
  `TestPlaqueShape_TheSevenValuesAreTheOnesShapeReturns` `go/build`'in, testin koştuğu
  bağlamda (GOOS/GOARCH, cgo ayarı, etiketler) seçtiği `GoFiles`'ı okur. CI testleri
  `CGO_ENABLED=1` ile koşar (`-race` linux/amd64'te cgo ister — `ci.yml`), ürün ikilisi ise
  `CGO_ENABLED=0` ile derlenir (`Makefile` `build`, Dockerfile'ın derlemesi); yani `!cgo`
  kısıtlı bir dosyadaki sabit CI'da pinden geçer ve sevk edilen ikilide bulunur. Pin ayrıca
  `return` ifadelerini okur, ertelenmiş (`defer`) bir fonksiyonun adlandırılmış sonuca
  yaptığını okumaz (kurgulanmış bir biçim). İkisi de kod incelemesinin konusu.

**Güvenlik iddiası — üç parça.**

- **Tehdit modeli:** bu ölçümler ve pinler, plaket okumasının SQL'ine, yetkilerine ve Go
  erişimcisine **kazara** giren bir değişikliğe karşıdır — bir tenant filtresini, bir bilet
  koşulunu, bir sütun yetkisini düşüren ya da bir anahtar sütununu (ifade içinde dahi) okumaya
  çalışan bir düzenleme — ve bir DSN sahibinin `tappa_operator` olarak yapabildiklerine (ölçülen
  kollar). Pini atlatmak için bilerek yazılmış kod ve sahibin (`tappa_owner`) yapabildikleri
  kod incelemesinin ve sayılı sınırların konusudur.
- **PART I — bugün sevk edilen kodun ölçülen davranışı** (dev Postgres 17.10, 2026-10-03; test ·
  girdiler · assert · onu kırmızıya çeviren mutasyon):
  - `TestOperator00030_TheDefinerCannotReadAPlaqueKey` · katalogdan türetilen `tags` anahtar
    sütunları, sır biçimli bütün tenant sütunları, tanımlayıcı olarak **yirmi yedi** ifade
    (1. tur: seçim listesi, `WHERE … app_key_ref IS NOT NULL`, toplama, `octet_length`, tam
    satır, `*`; 2. tur: `row_to_json(g)`, `g::text`, `to_jsonb(g)`, onun üzerinde `jsonb_each`,
    `g IS NOT NULL`, `pg_column_size(g)`, `(g).uid`, `(tags.*)`, anahtarla `ORDER BY`/`GROUP BY`/
    `IS DISTINCT FROM`/`NATURAL JOIN`/`max(octet_length(…))`/alt sorgu, `*`'lı CTE, `TABLE`,
    üç yazmanın `RETURNING *`'ı), üç `COPY` biçimi (tablo, sütun listesi, sorgu), `pg_stats`,
    `op_read_tenant_plaques`'ın iki değiştirilmiş kopyası, ikiz plaketler · türetilen liste tam
    olarak `aes_key_ref`, `app_key_ref`; ikisinde de SELECT/INSERT/UPDATE yok; yirmi yedi ifade
    ve üç `COPY` 42501 — yirmi dördü ve üç `COPY` anahtar sütunlarında SELECT yetkisi olmadığı
    için; üç yazmanın `RETURNING *`'ı ise anahtar yetkisini **yalıtmaz**: ayrıca `tags` üzerinde
    bir yazma yetkisi ister, tanımlayıcıda o da yoktur (`has_table_privilege` INSERT/UPDATE/DELETE
    = false), ve eksik iki yetkiden **her biri tek başına** reddeder — geri alınan işlemde
    ölçüldü: yalnız INSERT/UPDATE/DELETE verilince üçü 42501, yalnız iki anahtarda SELECT
    verilince üçü 42501, ikisi birden verilince üçü geçer (kapanış denetiminin ölçümü, 4. turda
    yeniden üretildi). Hangi denetimin önce düştüğü — PG17 `ExecCheckOneRelPerms`'e göre
    DELETE'te tablo yetkisi, UPDATE ve INSERT'te SELECT sütun denetimi önce — bir kaynak
    okumasıdır, ölçüm değil (4. tur, N1: 3. turun *"daha önce, hiç yazma yetkisi olmadığı
    için"* cümlesi aşırıydı; 2. tur onları "anahtar yetkisi" sayıyordu); `pg_stats`'ta sahip iki anahtar sütununun satırını görür, tanımlayıcı 0
    (izinli `status`'unkini görür — kontrol); değiştirilmiş gövde çalışırken 42501, değişmemiş
    kopya plaketlerin hepsini okur; hiçbir tanımlayıcı gövdesi anahtar adlandırmaz; ikizler uid
    dışında aynı satır; `tappa_operator` `tags`/`locations`'ta yetkisiz ve doğrudan okuma 42501;
    `tappa_app`'in çağrısı 42501 · U11, U12, U13, U19.
  - `TestOperator00030_TheFunctionAndItsExactSignature` · katalog · tam imza ve sonuç, sahip,
    `proconfig`, tek overload, EXECUTE yalnız `tappa_operator`; ileri, donan saat ve tüketim
    taramaları onu okudu ve bulgu yok; tür CHECK'i tam dört tür · U13, U15.
  - `TestOperator00030_TheDefinerExecutesOnlyItsOwnAndPublicFunctions` · bütün şemalardaki
    fonksiyonlar · bulgu 0; `resolve_tag_by_uid` kapalı; iki kontrol (resolver'a EXECUTE, PUBLIC'i
    alınmış bir fonksiyon) bildirilir.
  - `TestOperator00030_DownGivesBack00029AndUpTakesItAgain` · dosyanın Down/Up'ı işlem içinde;
    00029'un dosyasından türetilen küme; dört dal (tüketilmemiş `tenant_plaques` bileti,
    yalnız **tüketilmiş** bir bilet, yalnız **sonraki bir migration'ın** türü — Up'ı ve
    biletini bırakan Down'ı simüle edilir —, hiç bilet) ve L9'un zinciri · Down'ın koşulu ve iki
    CHECK'i tam olarak 00029'un kümesi; Down: okuma yok, `op_begin_read` 00029 gövdesi,
    tanımlayıcının listeleri ve ACL girdileri 00029'unkiler, genel bakış okur, `tenant_plaques`
    birinci aşaması 22023, yeni bilet 23514; ilk üç dalda `NOT VALID`, dördüncüde VALIDATED;
    zincirde 00029'un Down'ı 23514 (`tenant_plaques` biletiyle ve sonraki-tür biletiyle — L9);
    sonraki-tür bileti dururken 00030'un Up'ı yeniden 23514 (L11; 3. tur); Up yeniden 00030 ·
    D1–D5, SD6 (`AND consumed_at IS NULL`), SD7 (yalnız kendi türü — sonraki-tür dalı 23514),
    SD8 (kümeden bir tür eksik).
  - `TestOperator00030_PreconditionRefusesAWrongCluster` · on rol şekli · 55000 ve 00030 · D6.
  - `TestOperator00030_CallersTempTableIsNeverRead` · yedi tablo adının çağıranın geçici
    tablosuyla gölgelenmesi (GRANT adımıyla) · okuma ve yazma gerçek tablolarda.
  - `TestOpBeginRead_ThePlaqueKindBindsTheTenantAndNothingElse` · tür, on iki parametre reddi,
    altı ölü oturum, üç kontrol türü · tek satır + tek bilet + hash, 22023/28000 satırsız · U14.
  - `TestOpReadTenantPlaques_ReturnsOnlyTheNamedTenantsPlaques` · her durumdan plaketli iki
    tenant (2. tur: duvarda ve damgalı bir **kayıp** plaket dahil) · her okuma sahibin okumasına
    birebir eşit, ötekinin uid'i ve girişi yok; erişimcinin **her alanı** plaket plaket sahibin
    okumasına eşit (2. tur); ad, Total, sekiz plaketin şekli (A-1 ve damgalı kayıp dahil); okuma
    audit yazmaz · U1, U10, G1, G10, Ga, Gk (`LastCtr` hep 0), Gl (`created_at` ↔ `retired_at`),
    Gm (`LocationName` düşmüş).
  - `TestOpReadTenantPlaques_MatchesTheTenantsOwnList` · `tappa_app` olarak RLS altında
    `ListTagsForTenant` · aynı plaketler, aynı sıra, aynı sütunlar · U1, U9.
  - `TestOpReadTenantPlaques_AnUnknownTenantReadsNothingAndAnEmptyOneItsName` · bilinmeyen ve
    plaketsiz tenant · 0 satır / `ErrNoSuchTenant`; tek satır, NULL'lar, 0 · U2, U8, G7, G8.
  - `TestOpReadTenantPlaques_TheFirst200AndTheWholeCount` · 205 ve 200 plaket · 200 satır =
    sahibin listesinin ilk 200'ü, `plaque_count` 205; Total/Truncated · U7, U9, G6, G10.
  - `TestOpReadTenantPlaques_ATicketFromThisTransactionIsRefused` · A1/A2 · 28000 · U5.
  - `TestOpReadTenantPlaques_AForgedTicketIsRefused` · başka oturum, başka tenant, verilmemiş ve
    NULL bilet, aynı hash'in üç başka türü, envanter biletinin genel bakışa gösterilmesi ·
    28000, tüketim 0 · U4.
  - `TestOpReadTenantPlaques_RefusesEveryDeadSession` · altı ölü oturum · 28000 · U16.
  - `TestOpReadTenantPlaques_ExpiryIsTheWallClock` · dolmuş; savepoint ×3; uykusu içinde tek
    `DO` (0/3) · U6.
  - `TestOpReadTenantPlaques_TheReadWritesNoPlaque` · `ctid`/`xmin`/`last_ctr`, katalog,
    `AdvanceTagCounter` + tekrar · değişmez; yazma yetkisi yok; ilerleme ve `ErrNoRows` · U17.
  - `TestOpReadTenantPlaques_TwoPhaseLifecycle` · gerçek commit'ler · tek `read` satırı, geri
    alınan okuma yeniden okur, commit edilmiş tüketimden sonra 28000 · U18.
  - `TestTenantPlaques_OnThePoolTheTwoPhasesAreTwoTransactions` · üretim kurucusunun havuzu ·
    iki işlem, bilinmeyen id `ErrNoSuchTenant` + satır, tek işlemde ve bilinmeyen oturumda
    `ErrOperatorRefused`, işlenmiş tenant'ın envanteri · G5, G8, G9, U14.
  - `TestTenantPlaque_ShapeIsAClosedMapping` · dört şema durumu × duvar × damga çarpımının
    **on altı** hücresinin tamamı (tablonun çarpımı tam kapsadığı da denetlenir) + dokuz tuhaf
    durum değeri × dört hücre · izinli altı hâl kendi şeklinde (kayıp plaket dört hücrede de
    `lost`), yasak altı hücre ve her tuhaf değer `unrecognised` · G1–G4, Ga, Gb, Gc, Gd.
  - `TestPlaqueShape_TheSevenValuesAreTheOnesShapeReturns` · paketin ürün kaynağı (`go/build`'in
    `GoFiles`'ı), standart kütüphanenin `go/types`'ıyla **tip denetlenerek** (kaynak içe
    aktarıcı; yeni bağımlılık yok) · (a) paket kapsamındaki tipi `PlaqueShape` olan **bütün**
    sabitler — `go/build`'in testin **kendi** derleme bağlamında seçtiği dosyaların hangisinde,
    hangi yazımla (tipli, tipsiz + dönüşüm) — tam yedi (L12); (b) `Shape`'in
    **her** `return`'ü bu sabitlerden birine çözülen tek bir ad — literal, dönüşüm, çağrı
    (yardımcının dönüşü dahil), yerel ya da tipsiz sabit, değişken **bulgudur** (kabul edilen
    tek biçim adlandırılır; reddedilenler değil — fail-closed); (c) `Shape` yedisinin her birini
    döndürür; kontroller: sentetik paketlerde literal, dönüşüm, yardımcının dönüşü ve tipsiz sabit
    bulgu verir, tipsiz bir spec ve ikinci dosyadaki sabit sayılır · Gh, X7 (`return
    PlaqueShape("damaged")`), X8 (tipsiz `PlaqueDamaged = PlaqueShape("damaged")`), X9 (`return
    "damaged"`), X16 (başka dosyada sabit), X17 (`return lostShape()`). (3. tur, F1: 2. turun pini
    yalnız `operator.go`'nun sözdizimini okuyordu — tipli spec, ad dönüşü — ve dört yazım onu
    geçti.) Tehdit modeli: kazara sapma — bir hâlin bir yerde eklenip ötekinde unutulması; tip
    denetimli bir okumayı bilerek atlatan kod kod incelemesinin konusudur.
- **PART II — adıyla pinler ve yakaladıklarının tam listesi:**
  `TestOperator00030_TheFunctionAndItsExactSignature` — fonksiyonun tam argüman listesi ve
  sonuç tipi (dönüşe bir sütun giremez), sahip, overload, `proconfig`, dört rolün EXECUTE'u,
  tür CHECK'i; `TestOperator00026_PrivilegeMatrix` (genişletildi) — tanımlayıcının `tags` ve
  `locations` üzerindeki tam sütun listeleri, "asla" sütunlarının izin listesine girememesi,
  `public`'teki her ilişkide (görünümler dahil) liste dışı yetki; `TestLocations_WiFiSSIDNeedsNoNewGrantOrPolicy`
  — `locations`'ın sütun düzeyi ACL girdileri; `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays`
  — adı `op_read_` ile başlayan her fonksiyonun tüketen `UPDATE`'i (bu okuma dahil; U4, U5, U16,
  U18); `TestOperator00026_NoFrozenClock` (U6); `TestOperator00030_TheDefinerExecutesOnlyItsOwnAndPublicFunctions`
  — tanımlayıcının yabancı EXECUTE kümesi; `TestOperatorSQL_OnlyBoundParameters` — on üç sabit ve
  çağrıları; `TestPlaqueShape_TheSevenValuesAreTheOnesShapeReturns` — paketin tipi `PlaqueShape`
  olan sabit kümesi (yedi, adıyla) ve `Shape`'in her `return`'ünün o sabitlerden birinin adı
  oluşu; yakaladıkları: o kümeye bir sabit eklenmesi (testin derleme bağlamında seçilen her
  dosyada, her yazımla — L12), adla olmayan her
  dönüş (literal, dönüşüm, çağrı), dönmeyen bir sabit. Mutasyon tablosu (1. tur 37: 35 kırmızı,
  2 tasarım gereği yeşil — U3/L1, G11/L2; 2. tur 11 yeni, 11 kırmızı; 3. tur 5 yeni, 5 kırmızı,
  Gh yeni pine karşı yeniden kırmızı; dosya mutasyonlarının 1. turdakileri 2. tur ağacında
  yeniden koşuldu, sonuç aynı) OP-13 A kart düzeltmesinde.
- **PART III:** listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**OP-13 B fazı eki (2026-10-06, ekran, wiring ve okuma bütçesi).** Yukarıdaki notun *"ekran,
wiring ve `*OperatorDB` yöntemi B fazıdır"* cümlesi A fazının kaydıdır; B fazında tüketici
yazıldı: `internal/handler/operator/plaques.go` (tüketici arayüzü `PlaqueStore`, handler
`tenantPlaques`, sözlük `plaqueWords`), `web/templates/operatorpages/plaques.templ`
(`TenantPlaques`), `*OperatorDB`'ye `TenantPlaques(ctx, sessionHash, tenantID)` —
`return TenantPlaques(ctx, o.pool, …)`. Migration yok; bağımlılık yok. Kararlar, ölçümüyle:

1. **Rota `GET /operator/tenants/{id}/plaques`, konsolun grubunda** (host kapısı → güvenlik
   başlıkları → flood → same-origin, okuma kapısıyla → `requireOperator` → `sessionGate`).
   Envanter BİR tenant'ındır: genel bakış ona link verir, konsol vermez. ADR 0020 §4'ün
   *"Rotalar"* maddesi buna göre düzeltildi (OP-13 notu); `/operator/plaques` kayıtlı
   değildir ve yönlendiricinin kendi 404'ünü verir.
2. **Tek okuma, başlık dahil.** Başlıktaki ad `TenantPlaqueInventory.TenantName`'den gelir;
   ayrı bir genel bakış okuması yapılmaz (ikisi iki okuma, iki `read` satırı, iki okuma
   birimi olurdu). Görünür adı olmayan tenant OP-11'in yer tutucusuyla (`tenantBanner`, iki
   ekranın ortak yardımcısı) id'siyle adlandırılır. Okumanın döndürdüğü tenant yolun
   tenant'ı DEĞİLSE ekran 503'tür (`errPlaquesOfAnotherTenant`; internal/db'nin satır
   denetiminin handler'daki kopyası — başlık sorulmamış bir tenant'ı adlandırmasın).
3. **Durum sözlüğü kapalı ve fail-closed.** `TenantPlaque.Shape()`'in yedi değerinin her
   birine bir etiket, bir cümle ve bir ton (`plaqueWords`); A-1 kendi adıyla (*"On a wall,
   never encoded"*). Sözlüğün anahtar kümesi, internal/db'nin tipi `PlaqueShape` olan
   sabitlerinden go/types ile TÜRETİLEN kümeye eşittir (sekizinci şekil eklenirse kırmızı);
   sözlükte olmayan bir şekil `unrecognised`'ın sözüyle söylenir; `unrecognised` satır
   düşmez, saklı durumu `strconv.Quote` ile gösterir (boş değer, boşluk, harf büyüklüğü,
   kontrol karakteri görünür). Cümleler şemanın ve ürünün kendi kuralını söyler: takma
   damga ister (00025, `AssignTagToLocation`), emekli ve kayıp plakete dokunuş reddedilir
   (§5 satır 1, `sys:tag-not-active`). Emeklinin halefi olmayabilir (`tags.replaced_by`
   boş bırakılabilir; dev'de 18 340 emeklinin 8 968'i halefsiz, 2026-10-06), bu yüzden
   cümlesi (*"Taken out of service, with or without a replacement; taps on it are
   rejected."*) ikisini de iddia etmez; halef varsa satırın *"Replaced by"* olgusu söyler
   (2. tur, B7).
4. **Ton eşlemesi markanın sabit eşlemesidir, yeni zemin yok:** hizmette → `tally--mounted`
   (yeşil, `tally--active`'in bildirimine gruplandı), damgasız (A-1 ve stokta damgasız) →
   `tally--unencoded` (saffron), stokta → `tally--stock` (line), emekli/kayıp →
   `tally--withdrawn` (tomato), tanınmayan → `tally--unrecognised` (ink, durum olmayan ton).
   Kelime her zaman ink; kontrast (WCAG 2.1, sRGB, paper üstünde kompozit): yeşil-lite
   13,70 · saffron-lite 13,97 · tomato %10 13,99 · line %10 15,55 · ink %10 13,27 ·
   ink/paper 16,17 · ink %70 6,05 · tappa-green/paper 7,73 (`TestPlaqueScreen_TheChipsAndTextClearAA`).
   `app.css` (gitignore'lu, yeniden derlendi): 554 → 554 kural, beş seçici listesi
   genişledi, başka değişiklik yok; 50 800 → 50 887 bayt; yorumlardan doğan kural yok.
5. **Yazma eylemi yok:** sayfanın tek formu ve tek düğmesi çubuğun çıkışıdır; `hx-`
   özniteliği yoktur (ölçüldü).
6. **Hatalar (OP-11 B kalıbı):** bozuk id 404 *"That link does not name a tenant"* — store
   çağrısı yok, okuma birimi yok; `ErrNoSuchTenant` 404 (adını taşıyan `read` satırı
   yazılmış); `ErrOperatorRefused` oturum açmanın 303'ü; başka her hata 503 *"The plaques
   could not be loaded"* — log satırı tenant id'sini taşır, oturum hash'ini taşımaz.
7. **Zaman UTC'de render'da** (`utcStamp`); uid, zamanlar ve sayaç mono; kesilmiş envanter
   *"The first 200 of N plaques are listed"*; plaketsiz tenant *"This tenant has no
   plaques."* (docket çizilmez).
8. **OKUMA BÜTÇESİ (OP-11 B fazının devri, burada yapıldı).** Oturuma bağlı ikinci bir
   sınırlayıcı: `readLimit` 60 / 10 dk; her okuma handler'ı onu BİR kez, kendi retlerinden
   SONRA ve store'dan ÖNCE öder (`spendRead`: `legalPage`, `listTenants`, `tenantOverview`,
   `tenantPlaques`) — bozuk id, reddedilen terim, sayfa ya da form okuma birimi harcamaz.
   `sessionLimit` artık yalnız istek sayar ve OP-10'un 100'üne döndü (okumanın ikinci birimi
   artık oturum bütçesinin değil). Türetme (`surface.go`): okuma ≈ 4 destek vakası × ~5
   okuma + ~5 legal + ~5 liste ≈ 30, × 2 pay → 60; istek ≈ 30 okuma + ~5 konsol + ~2 yayın +
   ~3 ret ≈ 40, × 2,5 → 100. **Model bir tahmindir** (kullanım verisi yok) ve OP-11'inkine
   (~15 genel bakış + ~15 liste/arama + ~5 legal + ~10 plaket ≈ 45) göre payı 1,33'tür:
   o yürüyüşte bir pencerede ~30'dan fazla tenant'ı (genel bakış + plaket) süpüren operatör
   429 alır ve sonraki pencereyi bekler. **Bedeli — çalınmış oturum çerezi:** pencere başına
   60 okuma (en çok 3 000 liste satırı ya da 200'er plaketlik 60 envanter = 12 000 plaket
   satırı) ve okuma olmayan 40 istek; her okuma bir `read` satırı. **Yarış yok:** `Charge`
   kilit altında tek artırım ve ret onun döndürdüğü sayıyla verilir; paketin kaynağında
   `Allowed` adlı seçici yasaktır (kaynak pini, aşağıda — eşzamanlılık testi oku-sonra-yaz
   biçimini yalnız yarış tutarsa yakalar, pin her seferinde; 2. tur, B2).
9. **Wiring:** `operator.New`'e ayrı bir `PlaqueStore` yuvası (OP-11'in her store'a ayrı
   parametre kararı); `cmd/tappa`'nın `operatorStore`'u dört arayüzün birleşimi;
   `configuredSurface`'in store'u TAM dört kullanım (`arg0 of operatorAuthenticator`,
   `arg1`–`arg3 of operator.New`), `texts` `arg4 of operator.New`.

**Güvenlik iddiası — üç parça.**

- **Tehdit modeli:** Bu ölçümler ve pinler, plaket ekranının ve okuma bütçesinin koduna
  KAZARA giren bir değişikliğe karşıdır — bir anahtar sütununu ya da varlığını bir alana,
  bir satırı komşu bir duruma, başlığı başka bir tenant'a taşıyan, bir sınır denetimini ya
  da okuma birimini düşüren bir düzenleme — ve bir oturum sahibinin URL, yöntem ve başlıkla
  yapabildiklerine (ölçülen kollar). Pini atlatmak için bilerek yazılmış kod ve süreç
  dışındaki yüzeyler kod incelemesinin ve sayılı sınırların konusudur.
- **PART I — bugün sevk edilen kodun ölçülen davranışı** (2026-10-06; test · girdiler ·
  assert · onu kıran mutasyon — mutasyon tablosu OP-13 B kart düzeltmesinde):
  - `TestE2E_PlaqueScreenReadsThroughTheDefinerAndAuditsEachRead` (PostgreSQL) · seed Kebab
    Factory (ya da en çok plaketli tenant) · genel bakış ekranı linkler; ekranın satırları
    sahibin listesinin uid'leri ve sırası, her satırın çipi ve cümlesi sahibin
    sütunlarından çıkan şeklin sözü; görüntü başına TAM bir `read` satırı (satır sayısı =
    görüntü sayısı; 1. turda 1–3 aralığı kabul ediliyordu, 2. tur B1) — kapsam
    `tenant_plaques`, tenant adlı, sayfasız, `detail` `{}` — ve başka kapsamda satır 0;
    sahibin okuduğu bütün `aes_key_ref`/`app_key_ref` değerleri sekiz biçimde sayfada 0,
    32+ onaltılık hane dizisi 0; bilinmeyen id 404 + adını taşıyan satır; bozuk id 404 +
    satırsız; üç ölü oturum 303 + satırsız; her okumanın bileti tüketilmiş · D01db (başlık
    için genel bakış okuması), D02db (sayfada 32 haneli dizi), X03 (başarılı okumadan sonra
    ikinci `TenantPlaques`).
  - `TestPlaqueScreen_NoKeyReachesThePage` · `db.TenantPlaque`, `db.TenantPlaqueInventory`,
    `operatorpages.PlaqueRow`, `operatorpages.TenantPlaquesView` alan kümeleri tam liste;
    sahte store'da her plaketin yanında anahtar biçimli iki değer (44 ve 16 bayt) · sayfada 0
    (sekiz biçim), 32+ onaltılık dizi 0; CONTROL görünen bir alana konan anahtar bulunur ·
    K01, K02.
  - `TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible` · on bir
    plaket (yedi şekil, beşinci bir durum, boş durum, büyük harfli durum, yasak kombinasyon,
    halefsiz ve emeklilik zamansız bir emekli); her plaketin eklenme zamanı UTC+2'de saklı ·
    her satır kendi etiketi, ton sınıfı ve cümlesiyle, sırasıyla; dört tanınmayan satır saklı
    durumunu tırnaklı gösterir; iki emekli aynı sözle, *"Replaced by"* yalnız halefli olanda;
    zamanlar UTC'de (sayfada `11:30` yok), mono, `bdi`; tek form ve tek düğme çıkış · M17,
    M19–M23, T01–T05, T09, X08.
  - `TestPlaqueWords_NameEveryShapeAndNothingElse` · go/types ile türetilen yedi sabit ·
    sözlük anahtarları eşit, etiket/cümle boş ve ortak değil, bilinmeyen şekil
    `unrecognised` · M17, M18, M19.
  - `TestPlaqueScreen_NamesTheTenantAndRefusesABadPath` · `<script>`'li ad, altı görünmez ad,
    yedi bozuk id, sondaki `/`, ek parça, `%2F`/`%2f`, HEAD/POST/PUT/DELETE, bilinmeyen id,
    store hatası, oturum reddi, başka tenant'a cevap veren store · başlıkta ve `<title>`'da
    kaçışlı ad; tek okuma (`TenantPlaques` 1, başka okuma 0); genel bakış linki ve geri
    linki — genel bakışın linki *"See this tenant's plaques"* der, *"every plaque"* demez
    (ekran en çok 200 gösterir; 2. tur, B6); 404/405 store'suz; 503 log'unda tenant id var
    hash ve token yok; 303 · M03, M13–M16, M27, M29, T06, D01.
  - `TestPlaqueScreen_TheFirst200OfNAndAnEmptyTenant` · 205, 200, 3/1000, 1, 0 plaket ·
    *"The first 200 of 205"* mono, ilk 200 sırasıyla; tam sayı; tekil; boş cümle, docket
    yok · M24–M26, T07, T08.
  - `TestOperatorHeaders_ThePlaqueClassesCarryThePolicy` · C94–C103, 15 düşmanca başlık,
    düşmanca sorgu · tasarlanan başlık adları ve değerleri, yansıma yok, betik yok; C98 ve
    C102'de `TenantPlaques` 0, C103'te store 0 · M01–M03, M05b, M06, M07, M09, M13, M14, S02.
  - `TestReadBudget_TheReadsOfEveryScreenShareOneBudgetPerSession` · beş okuma rotasından
    12'şer okuma; altı ret türünden 5'er (bozuk id'li plaket ve genel bakış, sınır üstü
    terim, 1..1000 dışı sayfa, `maxFormBytes` üstü gövde, okunamayan form); aynı operatörün
    yeni oturumu · 60 × 200, her rotanın 61.'si 429 (yüklem +1, store +0) ve OTURUM AÇIK
    biçimde (çubuğun çıkışıyla — `problemTooMany(true)`), tek WARN kaydı `limit=60
    period=10m0s`; 35 konsol 200, 101. istek 429 (oturum açık biçim); 30 ret okuma birimi
    harcamaz; bütçe hesabın değil oturumun · M03, M04, M05b, M06–M10, M11b, M12b, S01–S03,
    X17, X20, X22 (2. tur, B3 ve B9).
  - `TestReadBudget_ConcurrentReadsOfOneSessionStopAtTheLimit` · tek oturumdan eşzamanlı 100
    okuma · 60 × 200, 40 × 429, store 60 · M05b, M06, M07, M09, S02; oku-sonra-yaz biçimini
    (X01) OLASILIKLA yakalar — ölçüm kart düzeltmesinde; her seferinde yakalayan aşağıdaki
    kaynak pinidir.
  - `TestTenantPages_AReadCountsTwiceAgainstTheSessionBudget`,
    `TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget` (OP-10/11 adları korunarak) · 60
    okuma, 61.'si okuma bütçesinden 429; istek bütçesi 100'de · M06–M09, S02 ve sırasıyla M12b,
    M10 (genel bakış, liste) ve M11b (legal).
  - `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` · A60–A66 (plaket ekranı,
    bilinmeyen/bozuk id, hata, oturum reddi, başka tenant'a cevap, okuma bütçesi) · G1–G17
    × R1–R10 × S1–S4, hasat `TenantPlaques` 65×1, `TouchOperatorSession` 198 · M27, D01, S03.
  - `TestPlaqueScreen_EachChipHasTheRuleTheContrastTestComputes` · ekranın yazdığı beş ton
    sınıfı · `input.css`'te her birinin tek kuralı, tonun zemini ve çerçevesi · C01, C02.
  - `TestTenantPlaques_OnThePoolTheTwoPhasesAreTwoTransactions` (internal/db) · yöntemin
    KENDİSİ üretim kurucusunun havuzunda: bilinmeyen id `ErrNoSuchTenant` + bir `read` satırı ·
    kendi mutasyonu yok; yöntemin argüman sadakatini `TestOperatorDB_EveryMethodDelegatesVerbatim`
    tutar (W02; aynı mutasyon bu testte YEŞİL kaldı, W02db — ölçüldü: nil tenant da
    bilinmeyen bir id'dir).
- **PART II — adıyla pinler ve yakaladıklarının tam listesi:**
  `TestOperatorDB_IsTheStoreAndNothingMore` — yöntem kümesi Store ∪ LegalStore ∪ TenantStore
  ∪ PlaqueStore ∪ Close (W03); `TestOperatorDB_EveryMethodDelegatesVerbatim` — on iki yöntem,
  argümanlar sırasıyla (W02, W03); `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` — öncül 13;
  `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator` — store'un dört kullanımı, `texts`
  beşinci argüman (W01); `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` — dokuz rota,
  on dört çift, C1–C103; `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` — on
  ekran kurucusu; `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` — render.go'nun
  her değişkeni `problemPages`'te (M28); `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute`
  — yirmi beş render, iki `{id}` rotası; `TestSurface_TheScreensOfLaterTasksAreNotMounted` —
  fatura ve audit ekranları ve `/operator/plaques` yönlendiricinin kendi 404'ünü verir;
  `TestPlaqueWords_NameEveryShapeAndNothingElse` — sözlük = türetilen şekil kümesi;
  `TestPlaqueScreen_NoKeyReachesThePage` — dört tipin alan listesi;
  `TestReadBudget_NoOperatorSourceAsksTheLimiterBeforeCharging` — paketin dosyalarında
  `Allowed` adlı seçici (alıcısı ne olursa olsun, çağrılsın ya da çağrılmasın; RB1) ve
  `httpx.Limiter`'ın `Allowed`'ını gösteren tanımlayıcı (RB2) yok; CONTROL `Charge`'ı
  gösteren seçiciler ≥ 4, biri `spendRead`'de (X01; 2. tur, B2).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**Sayılı sınırlar (OP-13 B)** — LP1–LP18 kart düzeltmesinde; burada en ağırları: **LP1**
sabit pencere sınırda iki katı patlamaya izin verir (kısa sürede 120 okuma); **LP3** model
tahmindir, OP-11'in yürüyüş modeline göre pay 1,33; **LP6** tip duvarı listelenmiş bir alana
kopyalanan anahtarı görmez — o yarı tanımlayıcının iki anahtarda SELECT'sizliği (A fazı) ve
imza pinidir, E2E yalnız okuduğu tenant'ın kendi anahtar değerlerini sekiz biçimde arar.
*(3. tur, güvenlik denetiminin DÜŞÜK bulgusu, 2026-10-06:)* sayfa taraması tırnaklı ya da
kaçışlı biçimi aramaz — saklı durum `strconv.Quote` ile basılır ve bir bayt dizisi orada
`\x..` kaçışlarıyla görünür (denetçi `Status`'a 16 rastgele bayt koydu; sekiz biçimin 0'ı
yakaladı); ve `TestPlaqueScreen_NoKeyReachesThePage`'in sayfa yarısı alan listesi pininin
ölçtüğünü YENİDEN ölçer — sahte store yalnız `TenantPlaque`'ı kopyalar, o yarı kendi başına
kırmızıya dönemez. Asıl koruma tip duvarı + tanımlayıcının kolon yetkisidir. Yol bugün
ulaşılamaz (ölçüldü, dev, salt-okuma: `has_column_privilege('tappa_operator', 'tags', …,
'SELECT')` `aes_key_ref`, `app_key_ref` ve `status` için false — rol `tags`'i yalnız
tanımlayıcıdan okur —; `tags_status_check` dört değer, yani bir anahtar `status`'a giremez);
**LP9** E2E'nin şekil kapsamı okuduğu tenant'ın tuttuğu şekillerdir (Kebab Factory: duvarda,
kayıp, A-1, emekli), diğerleri yalnız sahte store'la ölçülür; **LP18** *(3. tur)* sekme
başlığında bidi yalıtımı yok — OP-11'den miras, bu görevin diff'i getirmedi: `tenantScreen`
(`web/templates/operatorpages/chrome.templ`) tenant adını `<title>`'a templ'in kaçışıyla ama
yalıtımsız yazar ve `<title>` `bdi` taşıyamaz; banner ve konum adı `bdi` içindedir.
`tenants.name`'de biçim karakterlerini (ör. U+202E) engelleyen CHECK yok (ölçüldü: tablonun
dört CHECK'i ad dışı sütunlarda). Sonuç yalnız sekme başlığındaki metnin görsel sırasının
değişmesidir; hiçbir karar sekme başlığına dayanmaz.

## OP-14 uygulama notu (2026-10-06, A fazı — veri katmanı)

Uygulama: `db/migrations/00031_read_the_operator_audit_log.sql` + `internal/db/operator.go`
(dışa açık `OperatorAudit`, `OperatorAuditQuery`, `OperatorAuditEntry`, `OperatorAuditKind` ve
on bir değeri, `OperatorAuditKinds`, `MaxOperatorAuditPage`, `ErrOperatorAuditFilterRefused`,
`OperatorPasswordOK`; paket içi `readOperatorAudit`) + `db/queries/operator.sql` (belge) +
`internal/db/operatoraudit_test.go`. Ekran, wiring ve `*OperatorDB` yöntemi B fazıdır;
`password_ok`'un yazıcısı (`internal/operatorauth`, hesap başına tavanıyla) C fazıdır. Kararın
gövdesi değişmedi; uygulamanın karar verdiği yerler, adıyla:

1. **Ad §2 v 6'nın kuralıyla:** `op_read_audit(p_session, p_ticket, p_kind, p_page_number,
   p_page_size)`. §1'in *"görüntüleyici iki aşamalı `op_read_audit` ile okur"* cümlesi böylece
   uygulandı: `tappa_operator`'ın `operator_audit_log` üzerinde hâlâ **hiçbir** yetkisi yoktur.
2. **`op_begin_read` yerinde değiştirildi** (`CREATE OR REPLACE`): 00030'un Up gövdesi + bir
   değişken, kapalı küme satırı, `v_want` dalı ve yeni bir `ELSIF` dalı (diff ile ölçüldü).
   `operator_audit`'in parametre nesnesi **tam olarak** `{kind, page_number, page_size}`:
   `kind` `''` (her tür) ya da audit tür kümesinin bir üyesi; sayfa **1..1000** — **OFFSET
   sınırı** (log yalnız büyür; sayfa 1000'den eskisine bir tür filtresiyle gidilir — **ancak**
   o türün kendisi 200 000 satırı aşmadıysa: ürünün gösterebildiği en eski satır, filtresiz
   okumada tablonun, filtreli okumada o türün en yeni 200 000'inci satırıdır ve bir tür 200 000
   satırı aşınca ondan eskisine **üründen ulaşılmaz** — sınır L14); boy 1..200. Audit satırı:
   `target_scope = 'operator_audit'`, sayfa, `detail = {"filter": <tür>}` ya da `{"filter":
   "all"}` — kapalı bir kümeden bir değer, serbest metin değil (orkestratör kararı K14-3).
   Filtre ve sayfa bilet hash'ine bağlıdır.
3. **Okuma:** `operator_audit_log`, en yeni önce (`at DESC, id DESC` — `id` eşitliği bozar:
   rastgele, yani eşitliğin sırası anlamsız ama tam ve sayfadan sayfaya kararlı); gövdede
   `LIMIT least(boy, 200)` ve `OFFSET (least(sayfa, 1000) − 1) × limit` (birinci aşama
   ikisini de zaten reddeder; gövde buna güvenmez). Görüntüleyicinin **kendi** `read` satırı
   birinci aşamada commit edildiği için ilk sayfanın **ilk** satırıdır — **ancak** ondan daha
   geç tarihli bir satır yoksa: iki aşama arasında commit edilmiş bir satır ya da sahibin
   geleceğe tarihlediği bir satır (sınır L1, L2; ikisi de ölçüldü).
4. **Sütunlar (§2 ii) ve `detail`:** `audit_id, at, kind, session_id, actor_admin_id,
   actor_name, target_admin_id, target_admin_name, target_tenant_id, target_tenant_name,
   target_scope, page_number, page_size, search_class, filter_kind, legal_slug, legal_bytes,
   detail_recognised`. 🔴 **`detail` ham dönmez.** Tablo append-only olduğu için her sürümün
   yazdığı her şekil orada kalır — geliştirme veritabanında hiçbir sevk edilmiş kodun artık
   yazmadığı **beş şekil** var (OP-14 kart taslağının ölçümü, 2026-10-06'da yeniden ölçüldü:
   `tenants` okumasında `{}`, `{"search": true}`, `{"kind": "text"}`; `legal_versions` ve
   `tenant_detail` okumasında `{"search": "id"}`) — ve gelecekteki bir yazıcı `detail`'e
   yanlışlıkla bir değer koyabilir. Bu yüzden **kapalı bir şekil listesi** okunur: `(read,
   tenants, {"search": none|text|address|id})` → `search_class`; `(read, operator_audit,
   {"filter": all|<bir tür>})` → `filter_kind`; `(read, legal_versions|tenant_detail|
   tenant_plaques, {})`; `(legal_publish, —, {slug: dört slug'dan biri, document_id: küçük
   harfli uuid, bytes: 1..262144})` → `legal_slug`, `legal_bytes`; `(başka her tür, —, {})`.
   Başka her şey `detail_recognised = false`'tur ve değer sütunlarının hepsi `NULL` (**fail-
   closed**); satırın kendisi düşmez. `target_scope` yalnız beş okuma türünden biriyse döner.
   Dönen serbest metin yalnız başka tabloların adlarıdır (operatörün `display_name`'i,
   tenant'ın `name`'i) — ekranın kaçışlaması B'nindir.
5. **Tür kümesinin dört kopyası** — `operator_audit_log_kind_check`, `op_begin_read`'in filtre
   listesi, `op_read_audit`'in filtre şekli ve Go'nun `OperatorAuditKinds`'ı — bir listeye
   (`opAuditKinds`) ölçülerek bağlıdır: veritabanı tarafı
   `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree`, Go sabitleri tarafı **tip
   denetimli** `TestOperatorAuditKinds_TheTypedConstantsAreTheList` (OP-13'ün Gh dersi: paketin
   `go/build` `GoFiles`'ı `go/types` ile; tipi `OperatorAuditKind` ve `OperatorAuthEvent` olan
   **her** paket sabiti, hangi dosyada hangi yazımla). OP-15/OP-16 kümeyi genişletince dördünü
   birlikte genişletmek zorundadır — biri unutulursa kırmızı. Okuma türleri için aynısı:
   bilet CHECK'inin her türü `op_read_audit`'in bildiği bir kapsam olmak zorunda
   (`TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList`'in "every read kind" kolları) — OP-12'nin yeni okuma
   türü bu testi genişletmeden yeşil kalamaz.
6. **`password_ok` (orkestratör kararı K14-1):** tür CHECK'ine ve `actor_shape`'in **oturumsuz**
   koluna aynı migration'da girdi (ikisi birlikte; yalnız biri olsaydı INSERT 23514 ile
   düşerdi — kart taslağının T1'i); `op_record_auth_event` yerinde değiştirildi: kapalı kümesi
   altı tür, `password_ok` hesabı **yalnız id ile** adlandırır (adres ya da id'siz çağrı 22023,
   satır yok — argümanların kendisiyle karar verilir, durum kehaneti değildir), kilit sayacına
   **dokunmaz** (ne artırır, ne sıfırlar, ne kilidi uzatır — kilitli bir hesapta da ölçüldü).
   Bilinmeyen bir id hedefsiz bir satırdır, hata değil (00026'nın varlık kehaneti kuralı).
   🔴 **Kısıt yakalayıcısı eklendi (T1'in ikinci yarısı, ölçüldü):** fonksiyonun yakalayıcısı
   yoktu; zorlanmış bir CHECK altında aynı INSERT çıplak hâliyle 23514 ve `DETAIL`'inde başarısız
   satırı — hedef hesabın id'si dahil — döndürüyor
   (`TestOpRecordAuthEvent_AConstraintRefusalCarriesNoRow`'un kendi ölçümü). Artık kısıt
   yakalanır, sunucuya kısıtın adı ve SQLSTATE'i `LOG` olarak gider (değer değil), çağırana
   **sabit bir mesaj ve yakalanan SQLSTATE** döner — `DETAIL` yok; satır da sayaç da yazılmaz.
   **LOG satırının metni ölçülür (2. tur):** test, satırı bir testin okuyabildiği tek kanaldan
   — `client_min_messages = log` diyen çağıranın bildirimi olarak — yakalar ve iletisini
   **birebir** karşılaştırır (`op_record_auth_event: the audit row failed constraint "<ad>"
   (SQLSTATE <kod>); refused`), `DETAIL` ve `HINT` boş, `CONTEXT` yalnız fonksiyonun satırı;
   fonksiyonun yükselttiği hiçbir bildirimde çağrının id'si ya da adresi yok; varsayılan
   `client_min_messages` altında çağırana böyle bir satır gelmez. Sunucu logunun kendisi bir
   testçe okunmaz (sınır L13).
   28000 değil: bu bir operatörün reddi değil, bozuk bir yazmadır. Kapalı kümeyle CHECK'ler
   anlaştığı sürece hiçbir argüman bir kısıta ulaşmaz; ulaşan iki yol kümelerin ayrışması ve
   hedef hesabın arama ile yabancı anahtar denetimi arasında silinmesidir.
7. **Yetkiler:** `tappa_opdefiner` `operator_audit_log`'un `id` dışındaki on sütununda SELECT
   aldı (sütun düzeyinde; `id` 00027'nindir ve yeniden verilmedi); INSERT listesi 00026'nınki,
   `at` yine yok; UPDATE/DELETE/TRUNCATE yok. `tappa_operator` ve `tappa_app` hiçbir tablo
   yetkisi almadı. 00031 **bir** fonksiyon yaratır (`op_read_audit`) ve **iki**sini yerinde
   değiştirir (`op_begin_read`, `op_record_auth_event`); üçünde de EXECUTE yalnız
   `tappa_operator`'dadır (değiştirilen ikisinde yetki dosyada yeniden yazılır, `tappa_app`'te
   yok). Tanımlayıcının `op_*` dışı EXECUTE
   kümesi değişmedi (00030'un pini; bulgu 0). ⚠️ **Sahip `at`'e yazar** (ölçüldü:
   `has_column_privilege(tappa_owner, operator_audit_log, at, INSERT) = true` — tablonun sahibi
   ve dağıtılan topolojide süper kullanıcı). **Sonucu, adıyla:** sahibin yazdığı bir satırın
   zamanı sahibin iddiasıdır; geleceğe tarihlenmiş bir satır her filtresiz ilk sayfanın başında
   durur ve görüntüleyicinin kendi satırını aşağı iter (ölçüldü); görüntüleyici sahibin yazdığı
   satırı tanımlayıcınınkinden ayıramaz. Sınır 5 süper kullanıcının bu tabloya yapabildiğini
   zaten sayar; K14-2'nin (opadmin audit türleri) sahip satırları bunu bir ürün yoluna
   çevirecek — o kartın `at`'i sütun listesine koymaması (taslağın C3'ü) ve bir `BEFORE INSERT`
   tetikleyicisinin `at`'i duvar saatine zorlaması (sahibi de bağlar, `DISABLE` edilene dek)
   orada ölçülecek seçeneklerdir. *(OP-14 D, 2026-10-07, 00033 — **kapandı, iki katman
   birlikte:** opadmin'in ürettiği INSERT'in sütun listesi `(kind, target_admin_id)`'dir, `at`
   yok (`TestSQL_EachActionWritesOneAuditRowInItsDoBlock` pinler); ve `operator_audit_log_at_is_the_wall_clock`
   tetikleyicisi **her** yeni satırın `at`'ini `clock_timestamp()`'e zorlar — sahibin `INSERT`'i,
   `INSERT … SELECT`'i ve `COPY`'si dahil (`TestOperator00033_TheRowsTimeIsTheWallClockWhoeverWritesIt`).
   `has_column_privilege(tappa_owner, …, at, INSERT)` hâlâ `true`'dur (yetki değişmedi, değer
   ezilir). Kalan: tablonun sahibi tetikleyiciyi kapatabilir, düşürebilir ya da fonksiyonunu
   değiştirebilir — sınır 5'in yolu (üçü de tablonun ya da fonksiyonun sahipliğini ister, süper
   kullanıcılığı değil; ikisinin sahibi de `tappa_owner`);
   tetikleyici kapalıyken sahibin 2999 tarihi durur (aynı testin sayılı-sınır kolu). "OP-14 D
   uygulama notu" LD2.)*
8. **NOT VALID kuralı, iki yönde:** Down üç CHECK'i önceki kümelere döndürür — bilet CHECK'i
   00030'un dört türüne, audit tür CHECK'i 00027'nin on türüne, `actor_shape` 00026'nın iki
   koluna — ve her biri, **önceki kümenin dışındaki** bir türden satır/bilet varsa `NOT VALID`
   olur (00030'un kuralı; koşul *"bu dosyanın türü"* değil). Up da aynı soruyu sorar (00030'un
   L11'i burada kapandı): kendi kümesinin dışındaki bir türden satır/bilet varsa — sonraki bir
   migration'ın Down'ının bıraktığı — CHECK'ler `NOT VALID` eklenir; Down/Up zinciri bileşir
   (Down testinin 4. dalı, ölçüldü). **Tüketilmiş biletler de sayılır (2. tur, ölçüldü):**
   üretimde biletler tüketilmiş durur ve tüketilmiş bir bilet de CHECK'in reddettiği bir
   satırdır; koşul bu yüzden yalnız türdür. Down testinin 5. dalı (yalnız ürünün kendi okumasının
   tükettiği bir `operator_audit` bileti → Down'ın bilet CHECK'i `NOT VALID`) ve 6. dalı (yalnız
   sonraki bir migration'ın tüketilmiş bileti → Down'ın **ve** Up'ın bilet CHECK'i `NOT VALID`,
   Up bileşir) bunu ölçer; dört koşulun her biri **bütün** `WHERE` cümlesiyle pinlidir (ilk turun
   pini yalnız `ARRAY`'i okuyordu: `AND consumed_at IS NULL` eklenen bir koşul yeşil kalıyordu).
9. **Başka görevlerin testlerinde güncellemeler (zayıflatılmadı):** `TestOperatorSQL_OnlyBoundParameters`
   13 → 14 sabit/çağrı; `TestOperatorAccessors_TheCustomerRoleCannotUseThem` + `OperatorAudit` ve
   `password_ok`'lu `RecordOperatorAuthEvent`; `TestOperator00026_PrivilegeMatrix` tanımlayıcının
   `operator_audit_log` SELECT hücresi tam sütun listesiyle (`tappa_operator`'ınki boş kaldı);
   `TestOperator00030_TheFunctionAndItsExactSignature`'ın bilet tür pini *"00030'un dört türünü
   tutan kapalı küme"* oldu (00029'un pininin biçimi; HEAD'deki tam küme
   `TestOperator00031_TheFunctionsAndTheirExactSignatures`'da);
   `TestOperator00026_ArgumentsNeverComeBackInAnError` 22023 mesajı (*"failure"* kelimesi
   kümeden çıktı, çünkü küme artık bir başarısızlık olmayan tür taşır); yalnız yorum:
   `TestOpRecordAuthEvent_ClosedSetNoActorNoAddress`, `internal/operatorauth`'un sızıntı testindeki
   iki gerekçe metni (*"one of five constants"* → sayısız yazım).
10. **Down/Up ölçümü (2026-10-06, geliştirme veritabanı, Postgres 17.10):** `pg_dump
    --schema-only`, `\restrict`/`\unrestrict` satırları ayıklanarak, sha256 ilk 16 hane: v30
    `66338d9974f75bb3` (OP-13A'nın v30'u) → Up → v31 `126d47090054fd82` → Down → v30
    `66338d9974f75bb3` → Up → v31 `126d47090054fd82`. Her goose adımı ayrı komut, sürüm her
    adımdan önce ve sonra ölçüldü. Down'dan önce `operator_audit` bileti 0, `password_ok` satırı 0,
    on bir CHECK'in hepsi doğrulanmış: Down doğrulanmış dala girdi (`NOT VALID` dalı Down testinin
    2.–4. dallarında ölçülür). OP-11 ve OP-13'ün yetkileri döküm eşitliğinin içindedir.

**Sayılı sınırlar (OP-14 A):**
- **L1** — *"Görüntüleyicinin kendi satırı ilk satırdır"* iki aşama arasında daha geç tarihli
  bir satır commit edilmediyse doğrudur. Gerçek veritabanında testler bunu sıra biçiminde iddia
  eder: kendi satırından önceki her satır ondan daha geç tarihlidir (`opLeads`).
- **L2** — Sahip `at`'e yazar (md. 7): geleceğe tarihli bir sahip satırı ilk sayfanın başında
  durur — ölçüldü (`TestOpReadAudit_NewestFirstAndTheViewersOwnRowLeads`'in son kolu). *(OP-14 D,
  00033: **kapandı**, tablonun sahibinin tetikleyiciyi kapattığı, düşürdüğü ya da fonksiyonunu
  değiştirdiği yol dışında (OP-14 D notu LD2) — o test
  artık tetikleyiciyi kendi işleminde kapatarak ölçer; tetikleyici açıkken sahibin 2999 tarihli
  satırı saatle tarihlenir ve görüntüleyicinin satırı öndedir:
  `TestOperator00033_TheRowsTimeIsTheWallClockWhoeverWritesIt`.)*
- **L3** — Kapalı şekil listesi dışında kalan her şekil "gösterilmedi" okunur; yeni bir türün
  dolu `detail`'i, liste onu adlandırana dek gösterilmez (tasarım gereği). Okuma türleri ve
  filtre kümesi için liste genişletmesi testle zorlanır; **okuma dışı** yeni bir türün dolu
  `detail`'i için zorlayan bir test yoktur — o satır "tanınmadı" kalır.
- **L4** — Sınır 4'ün penceresi 30 sn (geri alınan okuma bileti yeniden okunur).
- **L5** — Maliyet, ölçüldü (2026-10-06, geliştirme veritabanı, 17 109 satır, ~282 sayfa;
  sayfanın ifadesi sabit değerlerle, `EXPLAIN (ANALYZE, BUFFERS)`, geri alınan işlemde):
  filtresiz ilk sayfa `operator_audit_log_at_idx`'i geriye yürür + artımlı sıralama, 51 satır
  okunur, 0,5 ms; sık bir tür (`totp_failed`) aynı yol + süzgeç, 763 satır atlanır, 0,8 ms. Hiç
  bulunmayan bir türün filtresi (`password_ok`) ve derin sayfa (sayfa 1000 × boy 200) planlayıcıyı
  **tam taramaya + sıralamaya** geçirir: 17 109 satır, 2,7 ms ve 9,6 ms. Fonksiyonun bütünü
  (sahte biletlerle, `tappa_operator` olarak): 21,3 / 9,6 / 5,6 / 11,1 ms. Nadir tür ve derin
  sayfa tabloyla doğrusal büyür; indeks eklenmedi (`(kind, at, id)` nadir türü kapatırdı — tablo
  büyüdüğünde yeniden ölçülecek bir eşik, bugünün kararı değil). Ölçülen planlar ilk çağrıların
  özel planıdır; havuzdaki uzun ömürlü bir bağlantıda planlayıcı genel plana geçebilir —
  ölçülmedi.
- **L6** — DSN sahibi herhangi bir hesap id'si için sahte `password_ok` basabilir (sınır 7'nin
  düzeltmesi); satır oturum açmaz, sayaca dokunmaz. Veritabanında bir tavanı yoktur; Go'daki
  hesap başına tavan C fazınındır ve DSN sahibi onu atlar.
- **L7** — Görüntüleyici adları tanımlayıcının (BYPASSRLS) yetkisiyle okur: `pending`/`disabled`
  operatörlerin adlarını da gösterir — giriş aramasının RLS'inin gizlediğini, audit'li bir okuma
  olarak (kart taslağının B4'ü; ekran B'nin).
- **L8** — `op_read_audit` bir tenant parametresi almaz; sayfanın satırlarının adlandırdığı her
  tenant'ın **adını**, satırın taşıdığı değerle okur — §3.2'nin *"bilerek bütün tenant'ları
  tarayan fonksiyonlar adıyla bellidir"* sınıfına bu ad okuması girer (sayfa ≤ 200 satır).
  Okumanın kendi `read` satırı hangi tenant'ların sayfada göründüğünü yazmaz (içeriksiz).
- **L9** — **Down zinciri ve `password_ok`** (ölçüldü, Down testinin 3. dalı): `password_ok`
  satırı varken 00027'nin uygulanmış Up'ı on türlü CHECK'i doğrulanmış ekler ve 23514 ile düşer;
  00027'nin Down'ı da `read`/`legal_publish` satırı yokken düşer. C fazı `password_ok` satırı
  commit etmeye başlayınca `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`'in yeniden
  Up adımı geliştirme ve CI veritabanlarında kırmızıya döner (C'ye devir).
- **L10** — Kısıt yakalayıcısının `LOG` satırı (kısıt adı + SQLSTATE) `client_min_messages =
  log` diyen çağırana da gider (sınır 14'ün ikinci kanalının aynı mekanizması; 2. turda
  ölçüldü: o çağırana tam bir satır, metni birebir; varsayılan ayarda hiç); yalnız kümeler
  ayrıştığında ya da bir yarışta doğar, normal işleyişte doğmaz. Satır çağırana yalnız kendi
  çağrısının kısıt adını ve kodunu söyler.
- **L11** — opadmin'in eylemleri hâlâ `operator_audit_log`'a yazılmaz (K14-2: ayrı kart).
  *(OP-14 D, 00033: **kapandı** — "OP-14 D uygulama notu".)*
- **L12** — Commit eden iki test koşu başına **+3** `read` satırı (yaşam döngüsü 1, havuz 2),
  **+2** `platform_admins` (`disabled`), iptal edilmiş oturumlarını bırakır; `password_ok`
  satırı commit edilmez. Ölçüldü (2026-10-06; veritabanı saatinden T0, yalnız bu iki testin tek
  koşusu, satırlar `target_scope = 'operator_audit'` üzerinden atfedilerek — o kapsamı yalnız bu
  kartın kodu yazar): 3 okuma satırı, 2 aktör, 2 oturum (ikisi iptal; audit satırı başvurduğu
  için silinmez), 2 hesap `disabled`, bilet 0, `password_ok` 0.
- **L13** — **Sunucu logunun kendisi bir testçe okunmaz** (kısıt: ajanlar sunucu logunu
  basmaz). Ölçülen, aynı `ereport`'un iletisidir; sunucu logu ona kendi satırlarını ekler.
  Geliştirme veritabanında bunlara ifade loglaması da dahildir (`log_statement = all`,
  `log_min_duration_statement = 0`, bağlı parametreler — `client_min_messages = log` diyen
  test bağlantısında görüldü: `$2 = '<hesap id>'`); bu sunucunun yapılandırmasıdır, fonksiyonun
  satırı değil — üretimde kapalıdır (yukarıda, 2026-09-26 ölçümü) ve bu kart onu değiştirmez.
- **L14** — **Görüntüleyicinin ulaşabildiği satır sayısı sınırlıdır** (güvenlik denetimi,
  DÜŞÜK, 3. tur). Sınır koddan ve kaçış denemesi E07b'nin ölçümünden (sahte biletle sayfa
  1001 → sayfa 1000'in satırı) türetildi; 200 000 satırla ayrıca ölçülmedi.
  - **Sınır:** sayfa en çok 1000, boy en çok 200 — `op_begin_read`'in sayfa reddi ve
    `op_read_audit`'in gövdesindeki `least(sayfa, 1000)` ile `least(boy, 200)`. Filtresiz
    okumada tablonun, bir tür filtresiyle o türün **en yeni 200 000** satırı görülebilir.
  - **Daha eskisine üründen yol yoktur:** `tappa_operator`'ın tabloda yetkisi yok, başka bir
    `op_read_*` yok.
  - **Kötüye kullanım yolu:** `op_record_auth_event`'in veritabanında tavanı yoktur (L6).
    DSN sahibi oturumsuz türlerden birinde ≈200 000 sahte satırla (ör. `password_ok`,
    `login_failed`) o türün gerçek satırlarını ve filtresiz okumada bütün eski satırları
    görüntüleyicinin ulaşamayacağı yere itebilir. `read` türü için aynı yol bir oturum hash'i
    ister: `op_begin_read` canlı bir MFA oturumu olmadan yazmaz.
  - **C fazının hesap başına tavanıyla ilişkisi:** o tavan Go'dadır (`internal/operatorauth`)
    ve yalnız sürecin kendi yazdığı satırları sınırlar. DSN sahibi `op_record_auth_event`'i
    doğrudan çağırır, Go'nun hiçbir tavanından geçmez (L6). Bu yüzden C'den sonra da sel
    veritabanı tarafında sınırsız kalır; C'nin tavanı yalnız sürecin kendisinin bir seli
    üretmesini engeller.
  - **Hafifletenler:** satır kaybolmaz (append-only; sahip yine okur). Sel ilk sayfada
    görünür, çünkü en yeni satırlar selin kendisidir; yani sessiz değildir. OP-14'ten önce hiç
    görüntüleyici yoktu.
  - **Kapatma bu kartın işi değil:** veritabanında bir yazma tavanı ya da daha eskiye ulaşan
    bir okuma (ör. zaman aralığı filtresi) ayrı bir karttır.

**Güvenlik iddiası — üç parça.**

- **Tehdit modeli:** bu ölçümler ve pinler, audit okumasının SQL'ine, yetkilerine, tür
  kümesinin kopyalarına ve Go erişimcisine **kazara** giren bir değişikliğe karşıdır — bir bilet
  koşulunu, bir şekil kuralını, bir sayfa sınırını, bir sütun yetkisini düşüren ya da tür
  kümesini bir kopyada genişletip ötekinde unutan bir düzenleme — ve bir DSN sahibinin
  `tappa_operator` olarak yapabildiklerine (ölçülen kollar). Pini atlatmak için bilerek
  yazılmış kod ve sahibin (`tappa_owner`) yapabildikleri kod incelemesinin ve sayılı sınırların
  konusudur.
- **PART I — bugün sevk edilen kodun ölçülen davranışı** (test · girdiler · assert; onu kırmızıya
  çeviren mutasyonlar OP-14 A kart düzeltmesinin md. 14'ünde — sayılan 64'ün 64'ü kırmızı —
  ve 2. turun üçü, F1, F1u, H7, md. 20–21'de):
  - `TestOperator00031_TheFunctionsAndTheirExactSignatures` · katalog · `op_read_audit`'in tam
    imzası ve sonucu (`detail` sütunu yok), sahip, `proconfig`, tek overload, EXECUTE yalnız
    `tappa_operator`; iki değiştirilen fonksiyonun kimliği; ileri, donan saat ve tüketim
    taramaları bulgusuz; üç CHECK HEAD'de tam ve doğrulanmış.
  - `TestOperator00031_TheDefinerReadsTheLogAndTheOperatorDoesNot` · katalog ve ifade ·
    tanımlayıcının SELECT/INSERT listeleri, `at` INSERT'i yok; `tappa_operator` doğrudan SELECT,
    `COPY`, `detail` SELECT'i 42501; `tappa_app` okuma ve `password_ok` yazımı 42501; sahibin
    `at` INSERT'i var; append-only sahibi de bağlar; tanımlayıcının yabancı EXECUTE'u 0.
  - `TestOperator00031_DownGivesBack00030AndUpTakesItAgain` · dosyanın Down/Up'ı işlem içinde,
    önceki üç dosyadan türetilen kümeler, altı dal (5. ve 6.: yalnız tüketilmiş bilet) + sayılı
    sınırlar; dört `NOT VALID` koşulu bütün `WHERE` cümlesiyle · md. 8, L9.
  - `TestOperator00031_PreconditionRefusesAWrongCluster` · on rol şekli · 55000 ve 00031.
  - `TestOperator00031_CallersTempTableIsNeverRead` · beş tablo adının gölgelenmesi (GRANT
    adımıyla) · okuma, yazma ve adlar gerçek tablolardan.
  - `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree`,
    `TestOperatorAuditKinds_TheTypedConstantsAreTheList` · md. 5.
  - `TestOperatorAudit_AnUnknownFilterIsRefusedWithoutARoundTrip` · küme dışı filtre gidiş-dönüşsüz
    `ErrOperatorAuditFilterRefused`; her üye ve `''` bir deyim.
  - `TestOpBeginRead_TheAuditKindBindsItsFilterAndPage` · tür, parametre retleri (sayfa 1001,
    `'all'`, SQL'li filtre dahil), altı ölü oturum, dört kontrol türü.
  - `TestOpRecordAuthEvent_PasswordOKNamesItsAccountAndTouchesNoCounter`,
    `TestOpRecordAuthEvent_AConstraintRefusalCarriesNoRow` · md. 6 (ikincisi `LOG` satırının
    iletisini `client_min_messages = log` kanalından birebir; L10).
  - `TestOpReadAudit_NewestFirstAndTheViewersOwnRowLeads` · md. 3, L2.
  - `TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList` · ürünün kendi taramasıyla
    (`readOperatorAudit`): listedeki her şekil değerini, listede olmayan her şekil — ölçülen beş
    şekil dahil — `false`'u ve `NULL`'ları verir; `detail`'e dikilen bilet biçimli, TOTP biçimli,
    token, adres ve arama terimi değerleri hiçbir satırın hiçbir metin alanında yok; bilet
    CHECK'inin her türü bilinen bir kapsam.
  - `TestOpReadAudit_PagesAreCappedOrderedAndBounded` · 1002 satır · boy 201 → 200, sayfa 2,
    sayfa 1001 → sayfa 1000'in satırı.
  - `TestOpReadAudit_ATicketFromThisTransactionIsRefused`, `TestOpReadAudit_AForgedTicketIsRefused`,
    `TestOpReadAudit_RefusesEveryDeadSession`, `TestOpReadAudit_ExpiryIsTheWallClock` · §2 v'nin
    iki aşama tablosu bu okuma için.
  - `TestOpReadAudit_TwoPhaseLifecycle`, `TestOperatorAudit_OnThePoolTheTwoPhasesAreTwoTransactions` ·
    gerçek commit'lerle, üretim kurucusunun havuzunda.
- **PART II — adıyla pinler ve yakaladıklarının tam listesi:**
  `TestOperator00031_TheFunctionsAndTheirExactSignatures` — fonksiyonun tam argüman listesi ve
  sonuç tipi (dönüşe `detail` ya da başka bir sütun giremez), sahip, overload, `proconfig`, dört
  rolün EXECUTE'u, üç CHECK'in tam metni; `TestOperator00031_DownGivesBack00030AndUpTakesItAgain` —
  Down'ın ve Up'ın dört `NOT VALID` koşulunun **bütün** `WHERE` cümlesi, Down'ın iki bilet ve
  iki audit tür CHECK'inin kümeleri, `actor_shape`'in dört kolu;
  `TestOpRecordAuthEvent_AConstraintRefusalCarriesNoRow` — kısıt yakalayıcısının `LOG`
  iletisinin tam metni; `TestOperator00026_PrivilegeMatrix` (genişletildi) —
  tanımlayıcının `operator_audit_log` sütun listeleri, `tappa_operator`'ın boş hücresi;
  `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays` — `op_read_audit`'in tüketen `UPDATE`'i;
  `TestOperator00026_NoFrozenClock` — iki değiştirilen ve bir yeni gövde;
  `TestOperatorAuditKinds_TheTypedConstantsAreTheList` — tipi `OperatorAuditKind` ve
  `OperatorAuthEvent` olan paket sabitlerinin değer kümeleri (testin derleme bağlamında seçilen
  her dosyada, her yazımla — OP-13'ün L12'si aynen); `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree`
  — CHECK, `actor_shape`'in oturumsuz kolu, iki fonksiyonun kabul kümeleri ve `OperatorAuditKinds`;
  `TestOperatorSQL_OnlyBoundParameters` — on dört sabit ve çağrıları.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**OP-14 B fazı eki (2026-10-07, ekran, wiring ve okuma bütçesi).** Yukarıdaki notun *"Ekran,
wiring ve `*OperatorDB` yöntemi B fazıdır"* cümlesi A fazının kaydıdır; B fazında tüketici
yazıldı: `internal/handler/operator/audit.go` (tüketici arayüzü `AuditStore`; handler'lar
`auditLog`, `filterAudit`, `readAudit`; kapalı sözlükler `auditKindWords`, `auditScopeWords`,
`auditSearchWords`), `web/templates/operatorpages/audit.templ` (`AuditLog`), konsoldan link
(`home.templ`), `*OperatorDB`'ye `OperatorAudit(ctx, sessionHash, q)` — `return
OperatorAudit(ctx, o.pool, …)`. Migration yok; bağımlılık yok; `input.css` değişmedi (derlenen
`app.css` tabanla bayt-aynı, 51 079 bayt — ekran yalnız var olan sınıfları kullanır). Kararlar,
ölçümüyle:

1. **Rotalar konsolun grubunda:** `GET /operator/audit` (her türün ilk sayfası; URL'den hiçbir
   şey okunmaz) ve `POST /operator/audit` (gövdede `kind` ve `page`; sayfalayıcı iki form,
   gizli alanlarla). Zincir aynı: host kapısı → güvenlik başlıkları → flood → same-origin
   (okuma kapısıyla) → `requireOperator` → `sessionGate`. PRG yok: POST sayfayı 200 ile döner,
   yeniden yükleme bir okuma ve bir `read` satırı daha. Tür kapalı bir kümeden bir değerdir,
   kişisel veri değildir; URL'den okumama kuralı yine de tek biçimde tutuldu (tenant aramasının
   kuralı). Alt yollar (`/operator/audit/`, `/operator/audit/x`) ve tenant'a göre audit
   (`/operator/tenants/{id}/audit` — K14-5) yönlendiricinin kendi 404'üdür.
2. **Sınır, store'dan ve okuma bütçesinden ÖNCE:** `kind` `""` ya da `db.OperatorAuditKinds`'ın
   bir üyesi — birebir (trim yok, harf büyüklüğü katlanmaz; `all` bir tür değil, kaydedilen
   filtrenin değeridir); sayfa `1..db.MaxOperatorAuditPage` ondalık rakamlarla (işaret, boşluk,
   üs yok); gövde `maxFormBytes` (16 KiB) üstü 413, okunamayan form 400. Ret store'u çağırmaz,
   okuma birimi harcamaz ve *"Nothing was read"* der.
3. **Hatalar:** `ErrOperatorRefused` → oturum açmanın 303'ü; `ErrOperatorAuditFilterRefused` →
   400 (sınır aynı kümeyi reddettiği için ulaşılamaz); başka her hata → 503 *"The audit log could
   not be loaded"*, log satırı türü, sayfayı ve hatayı (çağrı + SQLSTATE) taşır, oturum hash'ini
   taşımaz. A fazının devri 22023 için 500 öneriyordu; sınır sayfayı zaten reddettiğinden 22023
   ulaşılamaz ve onu ayırmak handler'a SQLSTATE okutmak (`pgconn` importu) demekti — orkestratör
   brief'inin kuralı (*"diğer hata → 503"*) uygulandı.
4. **Ekran (`screen` kabuğu) — ADR 0020 §9'un okunuşu.** Görüntüleyici bir tenant'a **girmez**:
   günlük her operatörün ve bir sayfası birçok tenant'ı adlandırır. Bu yüzden *"girdiği tenant'ı
   başlıkta adıyla"* cümlesi ona şöyle okunur: başlık banner'ı yok; satırın dokunduğu her tenant
   satırda **adıyla** (`bdi`, kaçışlı) ve genel bakışına linkle durur; görünür adı olmayan
   tenant *"Unnamed tenant"* ve id'siyle (linkli), hiçbir `tenants` satırının taşımadığı bir id
   *"A tenant id no tenant has"* ve id'siyle (linksiz). Satır docket kalıbında: zaman (UTC,
   saniyeye kadar, mono), türün sözcüğü, aktör (adı `bdi` içinde; oturum öncesi satırda
   *"Before sign-in"*; görünür adı yoksa *"an unnamed operator"* + id), hedef hesap (aynı
   kural), hedef tenant, kapsam (*"Of …"*), sayfa (*"Page n (m per page)"*), arama sınıfı
   (tanınmadığında *"Search:"* etiketiyle — 2. tur), filtre, belge (`/legal/<slug>`, mono) ve bayt sayısı, tanınmayan detay için *"Detail not
   shown"*, oturum id'sinin ilk sekiz hanesi (mono). Oturum hash'i yok — tipte alanı yok.
5. **Kapalı ve fail-closed sözlükler.** Tür (on bir; `password_ok` dahil — K14-1: yazıcısı C
   fazı, ekran yalnız adlandırır), kapsam (altı), arama sınıfı (dört), filtre (`all` + on bir
   tür), belge (`legal.Slugs`). Sözlükte olmayan değer bir küçük harfli jeton ise
   (`^[a-z][a-z0-9_]{0,62}$`: `target_scope` CHECK'inin biçimi — 00026, `^[a-z][a-z_]{0,62}$` —
   artı rakamlar; rakam, ileride adında sayı taşıyan bir kapalı küme üyesi — ör. sürümlü bir
   tür — kendisi olarak basılsın diye eklendi ve işaretleme, boşluk ya da yön değiştirici
   taşımaz; 2. turda düzeltildi: 1. tur bu biçimi *"CHECK'in biçimi"* diye anıyordu) ham hâliyle
   mono ve *"Unrecognised"* çipiyle çizilir; değilse yalnız çip — değer basılmaz. Satır düşmez,
   komşu bir türe okunmaz. Sözcükler hüküm değildir: hiçbiri iki satırı birleştirmez,
   *"compromised"* yoktur (K14-6). Anahtar kümeleri pinli: tür sözlüğü = go/types ile türetilen
   `OperatorAuditKind` sabitleri = `OperatorAuditKinds()`
   (`TestAuditWords_NameEveryKindAndNothingElse`); katalogdan okunarak kapsam sözlüğü = bilet
   tür CHECK'i = `op_read_audit`'in döndürdüğü kapsam listesi, sınıf sözlüğü = `op_read_audit`'in
   `tenants` listesi, tür sözlüğü = audit tür CHECK'i
   (`TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns`); veritabanısız yarısı:
   kapsam sözlüğü = migration'lardaki en yeni `op_read_audit` tanımının kapsam listesi
   (`TestAuditWords_NameEveryScopeTheNewestMigrationReturns`). *(3. tur)* 00032'nin okuma türü
   `tenant_billing` orkestratörün kararıyla bu fazda sözcüğünü aldı — *"a tenant's billing"*
   (satırda *"Of a tenant's billing"*, kapsam sözcüklerinin kalıbında); sonraki bir migration'ın
   yeni okuma türü iki pini de sözcük yazılana dek kırmızıya çevirir.
6. **`pending` ve `disabled` hesapların adları görünür (sınır L7, kart taslağının B4'ü).**
   Tanımlayıcı adları BYPASSRLS ile okur; giriş aramasının RLS'inin gizlediğini (sınır 15)
   oturumlu bir operatöre **bilerek** gösterir ve bu gösterim audit'li bir okumadır (her
   görüntüleme bir `read` satırı). Gerekçe: audit'in sorusu *"hangi hesaba karşı"*dır ve
   bekleyen ya da devre dışı bir hesaba karşı denemeler tam da görülmesi gerekendir. Ölçüldü
   (E2E): bekleyen ve devre dışı bir hesabın adresiyle yapılan giriş denemesi bir `unknown_email`
   satırı bırakır (`OperatorByEmail` yalnız aktifi görür; `op_record_auth_event` hesabı adresle,
   durumundan bağımsız bulur) ve görüntüleyici iki hesabı **adıyla** gösterir; adresleri hiçbir
   sayfada yoktur.
7. **Sayfa (K14-4) ve ekranın erişimi — sınır L14'ün ekran sayısı.** Boy 50 (tenant listesinin
   sabiti, `auditPageSize = tenantPageSize`; istemciden okunmaz), üst sınır 1000. Veritabanı
   boyu 200'e kadar kabul eder ve L14 o boyla **tür başına en yeni 200 000** satırı sayar; ekran
   50 kullanır, bu yüzden **ekranın gerçek erişimi bir filtre başına — filtresiz okumada
   tablonun — en yeni 50 000 satırıdır** (1000 × 50). Ölçüldü: 50 120 `login` satırlı sahte
   günlükte filtreli sayfa 1000 dolu, *"Next"* yok ve sayfa *"the newest 50,000 entries of the
   kind chosen"* der (`TestAuditScreen_PagesForwardOnlyAfterAFullPage`). L14'ün metni
   değişmedi; bu ek ekranın sayısını söyler: 50 000 ile 200 000 arasındaki satırlara bugün
   üründen yol yoktur (veritabanının izin verdiği boyu ekran kullanmaz) ve L14'ün kötüye
   kullanım yolu ekranda dört kat kısadır — DSN sahibinin bir türü ≈50 000 sahte satırla
   doldurması o türün gerçek satırlarını ekranın ulaşamayacağı yere iter. Hafifletenler
   L14'ünkilerdir (satır kaybolmaz, sel ilk sayfada görünür).
8. **Okuma bütçesi.** Her görüntüleme — `GET` ve her `POST` (filtre, sayfa) — `readLimit`'e BİR
   birim (`spendRead`, kendi retlerinden sonra, store'dan önce) ve `sessionGate`'te `sessionLimit`'e
   bir birim öder. Türetme (`surface.go`): destek penceresi ~30 okuma (OP-13 B); audit inceleme
   penceresi ~1 ilk sayfa + ~4 filtre + ~10 eski sayfa + bir destek vakası (~5) ≈ 20 okuma;
   büyüğün iki katı 60 — **`readLimit` değişmedi**. İkisinin karışığı (her destek vakasından
   sonra günlüğe bir bakış: ~8 okuma) ≈ 38 okuma, 60'ın ona payı **1,58** (OP-11'in yürüyüşü 1,33
   ile en dar kalır). `sessionLimit`'in payı yeniden hesaplandı: destek ~40 istek → 2,5; audit
   ~28 → 3,6; karışık ~48 → **2,08** — 100 değişmedi. **Model tahmindir**, kullanım ölçümü
   değildir. Ölçüldü (`TestAuditBudget_EachViewIsOneReadOfTheSessionsSharedBudget`): tek oturumda
   30 plaket görüntüsü + 15 `GET` + 15 `POST` = 60 × 200 ve 30 `OperatorAudit`; 61. `GET` ve
   `POST` 429 (yüklem +1, store +0, oturum açık biçim); ardından 38 konsol 200 ve 101. istek
   429; ikinci oturumun 20 reti (tür, 0/1001 sayfa, 413, okunamayan form) okuma harcamaz.
   **Bedeli — çalınmış oturum çerezi:** pencere başına en çok 60 audit sayfası = 3 000 audit
   satırı, her görüntüleme kendisi bir `read` satırı.
9. **Wiring:** `operator.New`'e ayrı bir `AuditStore` yuvası (beşinci argüman; `texts` altıncı
   oldu). `cmd/tappa`'nın `operatorStore`'u beş arayüzün birleşimi; `configuredSurface`'in
   store'u TAM beş kullanım (`arg0 of operatorAuthenticator`, `arg1`–`arg4 of operator.New`),
   `texts` `arg5 of operator.New`. `internal/operatorauth/surface_external_test.go` de
   `operator.New`'i çağırır: **orkestratörün mekanik yaması uygulandı** (2. tur, orkestratör
   kararı) — iki parça: sahte `OperatorAudit` (`surfTenants`'a, tip yorumunun bir satırıyla) ve
   `operator.New`'in yeni argümanı; paket derlenir. 1. turda bu madde yamanın uygulanmadığını
   söylüyordu (o turun kısıtı `internal/operatorauth`'a dokunmamaktı); paralel OP-14 C aynı
   dosyaya dokunduğu için birleştirme orkestratöründür.
10. **Kontrast** (WCAG 2.1, sRGB; `TestAuditScreen_TheTextClearsAA`'nın logu): ink/paper 16,17 ·
    ink %70/paper 6,05 · tanınmayan çip ink/(ink %10 ∘ paper) 13,27 · tappa-green/paper 7,73 ·
    tappa-green/porcelain 6,85 · paper/tappa-green 7,73 (2. turun *"Search:"* etiketi renk sınıfı
    taşımaz: ink/paper; `app.css` yeniden derlendi, yine tabanla bayt-aynı). Yeni renk ve yeni zemin yok; ekranın
    tek çipi `tally--unrecognised` ve `input.css`'teki tek kuralı (ink çerçeve, ink %10 zemin)
    aynı testte okunur. Dokunma hedefleri: seçici `op-input` (en az 44 px), *"Show"*
    `btn--primary`, linkler ve sayfalayıcı `op-link`.

**Güvenlik iddiası — üç parça.**

- **Tehdit modeli:** Bu pinler kazara sapmaya karşıdır; bir pini bilerek atlatmak kod
  incelemesinin konusudur. Ölçümler, audit ekranının, wiring'inin ve okuma bütçesinin koduna
  KAZARA giren bir değişikliğe karşıdır — bir sınır denetimini, bir okuma birimini ya da bir
  sözlüğün fail-closed dalını düşüren, bir değeri URL'den okuyan, bir adı `bdi` dışına ya da
  kaçışsız yazan, oturum hash'ini ya da tam oturum id'sini bir yüzeye taşıyan bir düzenleme — ve
  bir oturum sahibinin URL, yöntem, gövde ve başlıkla yapabildiklerine (ölçülen kollar).
- **PART I — bugün sevk edilen kodun ölçülen davranışı** (2026-10-07; test · girdiler · assert ·
  onu kıran mutasyonlar — tablolar OP-14 B kart düzeltmesinde; 2. turun koşusu 1. turun 41
  mutasyonunu ve 11 yenisini son ağaçta yeniden koştu, 52/52 kırmızı; aşağıdaki numaralar o
  koşunundur, katalog testininki 1. turun):
  - `TestOperatorHeaders_TheAuditClassesCarryThePolicy` · C104–C121, 15 düşmanca başlık,
    düşmanca sorgu (`kind`, `page` dahil) · tasarlanan durum, rota, başlık adları ve değerleri;
    yansıma yok, betik yok; C110, C114–C117, C120'de `OperatorAudit` 0, C113 ve C121'de store 0 ·
    M01, M03, M33, M34, M05, M06, M07, M09, M41, M11, M13.
  - `TestAuditScreen_EveryRowSaysWhatTheLogHolds` · yirmi dört satırlık günlük (on bir tür,
    bilinmeyen iki tür, kapsamlar, bilinmeyen ve dönmeyen kapsam, sınıf ve bilinmeyeni, üç
    filtre, iki slug, adlı/adsız/olmayan tenant, adlı/adsız aktör ve hesap, bekleyen ve devre
    dışı hesap adı), zamanlar UTC+2'de · görüntülemenin kendi satırı önde; her satırın
    olguları; tanınmayan arama sınıfı *"Search:"* etiketiyle (2. tur: yirmi beş satır, yedi
    çip; 3. tur: yirmi altı satır, `tenant_billing` kapsamı adıyla); sayfada `11:30`, tam oturum
    id'si, `<script` yok · M33, M34, M26, M14, M15, M16, M18, M19, M38, M20, M21, M22, M23, M24,
    M25, M28, M29, M31, M43, M39, M46.
  - `TestAuditWords_NameEveryKindAndNothingElse` · go/types ile türetilen on bir sabit · sözlük =
    sabitler = `OperatorAuditKinds()`; sözcük boş, ortak ya da hüküm değil; bilinmeyen tür
    jetonsa ham, değilse yalnız işaret (2. tur: `a-b`, `9abc`, `_abc` jeton değil; 3. tur: `z` +
    U+202E ve `z` + U+00E9 — ASCII dışı bayt — jeton değil) · M14, M15, M16, M17, E03, E04, X10.
  - `TestAuditWords_NameEveryScopeTheNewestMigrationReturns` (3. tur) · `db/migrations`'ın Up
    yarıları, `op_read_audit`'i yaratan ya da değiştiren sonuncusu (bugün 00032) · kapsam
    sözlüğü = o tanımın kapsam listesi; sözcük boş ya da ortak değil; CONTROL 00032 ya da
    sonrası, liste `operator_audit` ve `tenant_billing`'i taşır · M46.
  - `TestAuditScreen_TheBoundaryRefusesBeforeTheStore` · dokuz tür, on bir sayfa, 16 KiB gövde,
    okunamayan form; 2. tur: 64 bit taşan `18446744073709551621` · 400/413 ve store 0; CONTROL
    her tür ve `""`, dört sayfa, boy 50 · M01, M02, M03, M04, M33, M34, M05, M06, M31, E02.
  - `TestAuditScreen_ReadsNothingFromTheURL` · URL'de tür ve sayfa (`GET`, gövdesiz ve gövdeli
    `POST`) · store'a giden gövdeninki; formlar ve linkler sorgusuz · M33, M34, M06, M07, M08.
  - `TestAuditScreen_PagesForwardOnlyAfterAFullPage` · 120 ve 50 120 `login` satırı · Next ve
    Previous, gizli alanlar, filtre seçenekleri, sayfa 1000'in notu (*"50,000"*) · M33, M34, M06, M30, M31.
  - `TestAuditScreen_EscapesWhatOperatorsAndTenantsNamed` · aktör, hesap ve tenant adında
    `</bdi></a><script>`, sağdan-sola override · kaçışlı, `bdi` içinde, `<script` 0 · M33, M34, M22, M23.
  - `TestAuditScreen_NoCredentialFieldReachesThePage` · beş tipin alan listesi; üç görüntüleme ·
    çerez, hash, adres, TOTP kodu ve tam oturum id'si 0, önek var; CONTROL · M33, M34, M27, M26.
  - `TestAuditScreen_TheTextClearsAA` · paletten altı oran ≥ 4,5; tanınmayan çipin tek
    `input.css` kuralı · M42.
  - `TestAuditScreen_WearsTheDocketAnatomy` (2. tur) · süzülmüş günlüğün 2. sayfası · tek docket
    bölümü ve bütün satırlar içinde; `docket-label` başlık ve mono sayfa numarası; `op-input`
    seçici, `op-link` sayfalayıcı düğmeleri; `input.css`'te ikisinin `min-h-11`'i · M33, M34,
    M06, M27, M31, E17, E19, E20, E25, E26, M44, M45.
  - `TestAuditBudget_EachViewIsOneReadOfTheSessionsSharedBudget` · md. 8'in sayıları ·
    M01, M03, M04, M33, M34, M05, M06, M11, M12, M13, M39.
  - `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` · A67–A77 · G1–G17 × R1–R10 ×
    S1–S4; hasat `OperatorAudit` 6 × 1, `TouchOperatorSession` 208 · M01, M03, M33, M34, M05, M06, M09, M41, M10, M27, M11, M13, M20, M39.
  - `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` · otuz render · konsol günlüğü
    linkler, satır tenant'ın genel bakışını, formlar `/operator/audit`'e · M32.
  - `TestE2E_AuditScreenReadsTheLogThroughTheDefinerAndAuditsEachView` (PostgreSQL) · bir arama,
    bekleyen ve devre dışı hesabın adresiyle iki giriş denemesi, dört görüntüleme, ölü oturumlar
    · görüntülemenin kendi satırı önde ve zamanı satırın kendisi; `login` satırı adıyla; arama
    sınıfıyla, sözcük hiçbir yerde yok; iki hesap adıyla; hiçbir sayfada çerez, hash, tam
    oturum id'si, TOTP kodu, adres yok; ölü oturum 303 ve satırsız; görüntüleme başına bir
    `read` satırı ve bir tüketilmiş `operator_audit` bileti · M29, M39.
  - `TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns` (PostgreSQL) · katalog ·
    üç sözlük = üç küme (3. turdan beri goose 32'de yeşil) · M15, M19, M46.
  - `TestOperatorAudit_OnThePoolTheTwoPhasesAreTwoTransactions` (internal/db) · yöntemin
    KENDİSİ üretim kurucusunun havuzunda, `read` filtresiyle (2. tur) · boş olmayan sayfa, yalnız
    `read` satırları, kendi satırı aralarında, bir `read` satırı daha, `{"filter": "read"}` · kendi mutasyonu yok; yöntemin aktarımını
    `TestOperatorDB_EveryMethodDelegatesVerbatim` tutar (M37).
- **PART II — adıyla pinler ve yakaladıklarının tam listesi:**
  `TestOperatorDB_IsTheStoreAndNothingMore` — yöntem kümesi Store ∪ LegalStore ∪ TenantStore ∪
  PlaqueStore ∪ AuditStore ∪ Close; `TestOperatorDB_EveryMethodDelegatesVerbatim` — on üç yöntem,
  argümanlar sırasıyla; `TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor` — öncül 14;
  `TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator` — store'un beş kullanımı, `texts`
  altıncı argüman; `TestOperatorHeaders_TheWalkedRoutesEachHaveAClass` — on rota, on altı çift,
  C1–C121; `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` — on bir ekran
  kurucusu; `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo` — render.go'nun her
  değişkeni `problemPages`'te; `TestFormValues_TheListedSitesAloneRevealOrReadTheForm` —
  `r.PostForm` `filterAudit`'te yalnız `.Get("kind")` ve `.Get("page")` olarak birer kez,
  `URL.Query`/`FormValue`/`RawQuery` hiçbir audit yolunda yok;
  `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` — otuz render, konsolun audit linki;
  `TestAuditScreen_WearsTheDocketAnatomy` (2. tur) — docket bölümü, başlığın iki sınıfı,
  seçicinin ve sayfalayıcının sınıfı, iki 44 px kuralı;
  `TestSurface_TheScreensOfLaterTasksAreNotMounted` — `/operator/billing`, `/operator/plaques`,
  `/operator/audit/`, `/operator/audit/x`, iki `{id}` alt yolu yönlendiricinin kendi 404'ü;
  `TestAuditWords_NameEveryKindAndNothingElse` — tür sözlüğü = türetilen sabitler;
  `TestAuditWords_NameEveryScopeTheNewestMigrationReturns` (3. tur) — kapsam sözlüğü = en yeni
  `op_read_audit` tanımının kapsam listesi;
  `TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns` — üç sözlük = katalogdaki
  kümeler; `TestAuditScreen_NoCredentialFieldReachesThePage` — beş tipin alan listesi;
  `TestReadBudget_NoOperatorSourceAsksTheLimiterBeforeCharging` — paketin dosyalarında
  `Allowed` adlı seçici yok (audit.go dahil; CONTROL `spendRead`'de bir `Charge`).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**Sayılı sınırlar (OP-14 B)** — LB1–LB13 kart düzeltmesinde; burada en ağırları: **LB1** *"ilk
satır görüntüleyicinin kendi satırıdır"* bir sıra iddiasıdır (L1, L2); ekran onu varsaymaz, E2E
onu *"önündeki her satır daha geç tarihli"* biçiminde ölçer; **LB2** ekranın erişimi filtre
başına en yeni 50 000 satırdır (md. 7); **LB3** operatörün `display_name`'i ve tenant'ın adı
serbest metindir: bir sahip `display_name`'e bir adres yazarsa ekran onu ad olarak basar — sızıntı
sözleşmesi ekranın adresi başka bir kaynaktan getirmediğini ölçer, adın içeriğini değil
(öneri, bulgu değil: `opadmin`'in `validDisplayName`'i `@`'ye izin veriyor; ileride `@`'yi
reddetmek ucuz bir kalkandır);
**LB4** küçük harfli bir jeton biçimindeki tanınmayan değer ham basılır — kapalı kümelerin
(tür CHECK'i; `op_read_audit`'in kapsam, sınıf, filtre ve slug listeleri) dışında bir değerin bu
sütunlara girmesi o kümelerin ihlalidir ve ekran onu jeton biçimindeyse gösterir; **LB12**
kalıcı test verisi: E2E koşu başına üç operatör hesabı (`op10b-…`, `op14b-p-…`, `op14b-d-…`;
temizlikte `disabled`), beş oturum (iptal), login, logout, iki `unknown_email`, bir `tenants`
ve beş `operator_audit` `read` satırı bırakır (biletler silinir) — ölçüldü (2026-10-07, geliştirme
veritabanı, salt-okuma sayım, tek koşu çevresinde): hesap +3 (`disabled` olmayan 0), oturum +5
(iptal edilmemiş 0), audit satırı +10, bilet 0 → 0, `tenants` ve `tags` +0; A'nın havuz testi yöntemi de
sürdüğü için koşu başına 2 → 3 `read` satırı bırakır (sınır L12'nin sayımı B'den itibaren +4);
**LB13** *(2. tur)* `internal/operatorauth/surface_external_test.go`'ya orkestratörün mekanik
yaması uygulandı (md. 9; iki parça, paket derlenir); paralel OP-14 C ile aynı dosyanın
birleştirmesi orkestratöründür.

*2. tur (2026-10-07; üçüncü gözün RED'i — tek bloklayan bulgu metindi).* Düzeltilenler: md. 9 ve
LB13 (yama uygulandı); md. 5'in jeton biçimi (CHECK'in biçimi + rakam, nedeniyle); tanınmayan
arama sınıfına *"Search:"* etiketi (md. 4, md. 10); LB3'e `@` önerisi. Eklenen pinler: ekranın
docket anatomisi (`TestAuditScreen_WearsTheDocketAnatomy`: tek docket bölümü, `docket-label`
başlık ve mono sayfa numarası, `op-input` seçici, `op-link` sayfalayıcı ve ikisinin 44 px
kuralı), jeton biçiminin üç kenarı (tire, baştaki rakam, baştaki alt çizgi —
`TestAuditWords_NameEveryKindAndNothingElse`), 64 bit taşan yirmi haneli sayfa
(`TestAuditScreen_TheBoundaryRefusesBeforeTheStore`), havuz testinin boş olmayan sayfa öncülü
(`TestOperatorAudit_OnThePoolTheTwoPhasesAreTwoTransactions` artık `read` filtresiyle: fikstür
oturumu `login` satırı yazmadan açar). Mutasyon tablosu OP-14 B kart düzeltmesinin 2. turunda.
Geliştirme veritabanı 2. turda goose 32'dir (OP-12 A): `tenant_billing` bilet kümesindedir ve
`TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns` orada tasarım gereği
kırmızıdır — sözlük fail-closed kalır, sözcüğü OP-12 B ekler.

*3. tur (2026-10-07; güvenlik denetimi ONAY, orkestratörün üç küçük maddesi).* Taban OP-12 A'nın
commit'i `d7775b7`'ye taşındı (00032 dalda); B'nin satırları değişmeden uygulandı, OP-12 A'nın
metni bayt bayt korundu (kanıt kart düzeltmesinin 3. turunda). Önceki paragrafın son cümlesinin
yerine: `tenant_billing`'in sözcüğünü bu faz ekledi (md. 5) ve katalog testi goose 32'de yeşil;
kapsam sözlüğüne veritabanısız bir pin eklendi
(`TestAuditWords_NameEveryScopeTheNewestMigrationReturns`). `printableToken`'ın ASCII dışı baytı
reddetmesi pinlendi (`TestAuditWords_NameEveryKindAndNothingElse`: `z` + U+202E ve `z` +
U+00E9, beklenen yalnız çip). Başka davranış değişmedi; adlardaki bidi kontrol karakterleri ve
okumanın `statement_timeout`/context pini kart düzeltmesinde devir (OP-11/13/14 ortak).

## OP-12 uygulama notu (2026-10-07, A fazı — veri katmanı)

Uygulama: `db/migrations/00032_read_billing_from_the_operator.sql` + `internal/db/operator.go`
(dışa açık `TenantBilling`, `TenantBillingTimeline`, `TenantBillingMonth`,
`MaxTenantBillingPage`, `TenantBillingMonthsPerPage`; paket içi `readTenantBilling`,
`errBillingOfAnotherTenant`) + `db/queries/operator.sql` (belge) + `db/queries/billing.sql`'in
sonuna tek bir yorum cümlesi + `internal/db/operatorbilling_test.go` ve
`internal/db/operatorbilling_external_test.go`. Ekran, wiring ve `*OperatorDB` yöntemi B
fazıdır. Kararın gövdesi değişmedi; uygulamanın karar verdiği yerler, adıyla:

1. **Ad §2 v 6'nın kuralıyla:** `op_read_tenant_billing(p_session, p_ticket, p_tenant_id uuid,
   p_page_number integer)`.
2. 🔴 **Aritmetiğin üçüncü kopyası.** `db/queries/billing.sql` *"THE ARITHMETIC IS NOT WRITTEN
   TWICE"* der: önizleme (`PreviewBillingPeriod`) ve kapama (`CloseBillingPeriod`) 00016'nın beş
   fonksiyonu üzerinde aynı üç CTE'dir. Bu okuma o yapıştırıcının **üçüncü** kopyasıdır; beş
   fonksiyon **tek tanım** olarak kalır, burada da orada çağrıldığı gibi çağrılır. Ortak bir
   `RETURNS TABLE` fonksiyonu elendi: sqlc v1.28 onu tipleyemez (ADR 0002 md.7) ve tenant'ın
   kendi ifadelerini yeniden yazmak bu görevin işi değildir. 🔴 **Kopyaları bir arada tutan şey
   bir TESTtir, yorum değil:** `TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure` okumanın
   döndürdüğü **her** ay için tenant'ın kendi yolunu — üretilmiş store'un `GetBillingPeriod`'ını,
   dondurulmuş satır yoksa `PreviewBillingPeriod`'ını, `tappa_app` olarak tenant bağlamında —
   sorar ve alan alan karşılaştırır (md. 4). Bir kopyada yapılıp ötekinde yapılmayan bir
   değişikliği **kendi fikstürlerinde görünüyorsa** yakalar: üç zone, kurduğu kadro ve fiyat
   kenarları, UTC ay sınırının iki yanında kayıt olmuş iki tenant (2. tur) ve koştuğu anda okunan
   aylar. Yalnız **başka bir anda** görünen değişikliği — içinde bulunulan ayın tenant'ın
   zone'undan başka bir zone'da alınması — o test göremez (UTC'yle zone'un ayı yalnız bir ay
   sınırına bir gün kadar yakınken ayrılır); onu `TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant`
   yakalar (2. tur notu). Başka her biçim kod incelemesinindir — iki test tamlık iddia etmez.
   `billing.sql`'in başlığına değil **sonuna** tek bir cümle yazıldı:
   başlıktaki ya da iki ifade arasındaki her yorum sqlc tarafından `internal/store`'a kopyalanır
   (ölçüldü, sınır L9) — sorgu metni ve üretilen kod değişmedi.
3. **Okumanın şekli:** sayfa, tenant'ın **kendi** zone'unda on iki yerel aydır, en yeni önce;
   sayfa 1 tenant'ın içinde bulunduğu ay (`date_trunc('month', clock_timestamp() AT TIME ZONE
   <zone>)`) ve önceki on biri, sayfa k on iki × (k − 1) ay geriden başlar. Her ay üç şeyden
   biridir ve satır hangisi olduğunu söyler: **dondurulmuş** (`billing_periods`'ta satır var:
   her rakam o satırdan **okunur**, hiçbiri yeniden hesaplanmaz — `GetBillingPeriod`'ın kuralı;
   `first_chargeable_month` `NULL`, `closed_at` satırınki, `after_signup` ve `period_has_ended`
   doğru — internal/domain/billing'in `frozenDraft`'ıyla aynı), **canlı** (satır yok ve ay
   kaydın yerel ayında ya da sonra: `PreviewBillingPeriod`'ın aritmetiği, `currency` `NULL`,
   `period_has_ended` = `to_at <= clock_timestamp()`), **kayıttan önce** (`after_signup = false`,
   **bütün** rakamlar `NULL` — sıfır fatura değil). Dondurulmuş satır, kayıt ayının bugün nasıl
   okunduğundan bağımsız kazanır (zone sonradan değişirse de; tenant'ın `Book.Period`'ı da öyle
   cevap verir). Bilinmeyen id **sıfır satır** okur (`ErrNoSuchTenant`; okuma satırı zaten
   commit edilmiş), bilinen tenant **tam on iki**.
4. **Sütunlar (§2 ii):** `tenant_id, tenant_name, period_month, after_signup, frozen,
   period_from, period_to, period_timezone, plan, first_chargeable_month, free_period,
   employee_count, unstamped_employees, unit_price, currency, amount_due, closed_at,
   period_has_ended`. **`closed_by` YOK** (orkestratör kararı K12-6: kapatan tenant'ın
   yöneticisidir; operatörün sorusu ne zaman ve olup olmadığıdır). Çalışan adı, satırı ya da id'si
   **yok** — yalnız sayım. 🔴 **Para `numeric`'tir, float değil:** `RETURNS TABLE` tip
   değiştiricisini düşürür ve dayatmaz — ölçüldü (2026-10-07, 2. tur; geri alınan tek işlemde bir
   `pg_temp` fonksiyonu, `scratchpad/op12a/t4_typmod.sql`): `RETURNS TABLE (x numeric(12, 2))`
   katalogda `TABLE(x numeric)` okunur ve `0::numeric` ölçek 0, `1.5` ölçek 1,
   `1.5::numeric(12, 2)` ölçek 2 döner. Bu yüzden canlı tutarın ölçeği ifadede, önizlemenin kendi
   `::numeric(12, 2)` dönüşümüyle konur; dondurulmuş değerler sütunlarının ölçeğini taşır.
5. **Canlı aritmetik, `PreviewBillingPeriod`'ınki terim terim** — aynı beş fonksiyon, aynı
   argümanlar, aynı karşılaştırmalar; değişen **biçim**dir: kadro **bir kez** okunur ve aya göre
   gruplanır (önizleme her ay için okur), ay sınırları ay başına bir kez hesaplanır
   (`MATERIALIZED`: planlayıcıya bırakılınca çalışan başına süzgecin içine taşındı — ölçüldü),
   uygunsuz damga sayımı sayfa başına bir kez. Maliyet sınır L5'te.
6. **Kemer — her tablo referansı tenant'ı adlandırır** (§3.2): `t.id = p_tenant_id`,
   `b.tenant_id = p_tenant_id` (yoksa başka bir tenant'ın aynı ayın dondurulmuş satırı sayfaya
   katılır), `e.tenant_id = p_tenant_id` (yoksa veritabanındaki bütün çalışanlar sayılır).
   `TestOpReadTenantBilling_ReturnsOnlyTheNamedTenantsFigures` iki tenant'la sürer.
7. **`op_begin_read` yerinde değiştirildi** (`CREATE OR REPLACE`): 00031'in Up gövdesi + kapalı
   küme satırı + yeni bir `ELSIF` dalı (diff ile ölçüldü: −1 +23 satır). Parametre nesnesi
   **tam olarak** `{tenant_id, page_number}`: tireli uuid (iki harf büyüklüğü; hash'lenen metin
   uuid değerinden), sayfa **1..5** — **içeriksiz bir kural** (orkestratör kararı K12-3: beş
   sayfa × on iki ay = altmış ay = `billing.HistoryCap`, tenant'ın kendi geçmiş derinliği); sınırsız
   bir sayfa tarih aritmetiğini okuma satırı commit edildikten **sonra** taşırırdı (kart
   taslağının T7'si). Gövde de sayfayı sınırlar (`greatest(1, least(coalesce(sayfa, 1), 5))` —
   00031'in `least(…, 1000)`'i gibi, birinci aşamaya güvenmez). Audit satırı:
   `target_scope = 'tenant_billing'`, `target_tenant_id`, sayfa, `page_size` 12, `detail = {}`.
   Birinci aşama tenant'a yine **bakmaz** (B14'ün okuma hâli).
8. **`op_read_audit` yerinde değiştirildi:** 00031'in gövdesi, iki listeye `tenant_billing`
   eklenerek (diff: −2 +2) — dönen kapsam listesi ve `detail`'i boş nesne olan okuma türleri. OP-14
   notunun md. 5'i bunu zorlar: bilet CHECK'inin her türü `op_read_audit`'in bildiği bir kapsam
   olmak zorundadır (`TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList`, "every read kind";
   ölçüldü: `op_read_audit` 00031'in listelerine döndürülünce o test kırmızı — mutasyon A1). B
   fazına devir: görüntüleyicinin kapsam etiket haritası kapalıdır;
   `tenant_billing` için etiket eklenmezse kapsam *"unrecognised"* görünür.
9. **Tanımlayıcının yeni yetkileri — yalnız yeni sütunlar, sütun düzeyinde:** `tenants`
   (timezone, price_per_employee_month), `employees` (activated_at, deactivated_at — billable
   yükleminin dört argümanı, 00029'un tenant_id ve status'uyla), `billing_periods` (döndürülen
   sütunlar ve süzgeç için tenant_id; `id`, `created_at`, `closed_by` **değil**). Yalnız SELECT:
   tablo append-only'dir ve tanımlayıcı bir ay kapatamaz. 00029'un sütunları **yeniden
   verilmedi**; Down onları almaz ve `REVOKE ALL` yazmaz (00030'un tuzağı: genel bakışın
   sayımları 42501'e dönerdi). `employees.activated_at/deactivated_at` kişisel veridir (sınır 9):
   tanımlayıcı okur, **döndürmez**.
10. 🔴 **Tanımlayıcının `op_*` dışı EXECUTE kümesi artık adlı bir listedir** (kart taslağının
    T3'ü): 00016'nın beş fonksiyonu PUBLIC'in EXECUTE'unu almıştı (00016 bölüm 2b), 00032 onları
    tanımlayıcıya verir. Dördünü okuma doğrudan çağırır; beşincisini
    (`tappa_employee_lifecycle_status`) hem okuma (uygunsuz damga sayımı) hem
    `tappa_employee_is_billable`'ın gövdesi çağırır — o gövde çağıranı olarak koşar (SECURITY
    INVOKER), yani EXECUTE olmadan billable yüklemi 42501 ile düşer (ölçüldü,
    `TestOperator00032_TheDefinerExecutesExactlyTheFiveBillingHelpers`; okuma da 42501). Hiçbiri SECURITY DEFINER
    değildir, hiçbiri bir ilişki okumaz. **Pin genişletildi:** 00030'un kuralı (*"tanımlayıcının
    çağırabildiği, PUBLIC'in çağıramadığı ya da SECURITY DEFINER olan yabancı fonksiyon"*) **bir
    fonksiyonun ACL'inde tanımlayıcıyı adıyla anan girdiyi** görmüyordu — PUBLIC'in zaten
    çağırabildiği bir fonksiyona tanımlayıcıya verilen EXECUTE o kurala yeşil kalır. Ölçüldü:
    00032'den önceki ağaç (`2bfe314`), 00032'nin beş grant'ı aynı işlemde geri alınıp
    `tappa_forbid_mutation()`'a tanımlayıcıya adıyla EXECUTE verildiğinde 00030 ve 00031'in
    tanımlayıcı pinleri **YEŞİL** kaldı (mutasyon X0; kontrol X0c: PUBLIC-dışı
    `resolve_tag_by_uid` aynı ağaçta kırmızı); genişletilmiş pinde aynı mutasyon kırmızı (E2).
    `opDefinerForeignExecFindings` artık üç kural taşır: listenin dışındaki PUBLIC-dışı ya da
    definer fonksiyon, listenin dışındaki ACL girdisi, ve listede olup tanımlayıcının
    çağıramadığı fonksiyon — liste kümedir, tavan değil.
11. **NOT VALID kuralı, iki yönde** (00031'inki): Up — altı türün dışında bir türden bilet
    varsa CHECK `NOT VALID`; Down — 00031'in beş türünün dışında bir türden bilet varsa
    (tüketilmiş ya da değil, bu dosyanınki ya da sonraki bir migration'ınki) `NOT VALID`. İki koşul
    da **bütün** `WHERE` cümlesiyle pinlidir. Zincir aşağı doğru bileşir: bir `tenant_billing`
    bileti varken 00032 → 00031 → 00030 Down'ları 23514'süz koşar (00031 ve 00030'un Down
    koşulları da *"önceki kümenin dışı"*dır) — ölçüldü, Down testinin zincir dalı.
    **Down/Up ölçümü (2026-10-07, geliştirme veritabanı, Postgres 17.10):** `pg_dump
    --schema-only`, `\restrict`/`\unrestrict` satırları ayıklanarak, sha256 ilk 16 hane: v31
    `126d47090054fd82` (OP-14 A'nın v31'i) → Up → v32 `20b156a752860d78` → Down → v31
    `126d47090054fd82` → Up → v32 `20b156a752860d78`. Her goose adımı ayrı komut, sürüm her
    adımdan önce ve sonra ölçüldü; veritabanının 31'de kaldığı pencere ≈22 sn. (Down'un hangi
    dala girdiği bu döngüde ayrıca ölçülmedi; iki dal da Down testinde ölçülür.)
12. **Ön koşul:** 00026–00031'in rol denetimleri aynen; **sunucu** PostgreSQL 17 ya da sonrası
    (ölçümlerin yapıldığı sürüm — geliştirme 17.10; CI ve üretim `postgres:17`; bir taban, eşitlik
    değil); ve **00031'in izleri** — değiştirilen iki fonksiyon 00031'in kimlikleriyle var ve
    tanımlayıcının, beş fatura fonksiyonu 00016'nın kimlikleriyle var, bilet CHECK'i
    `operator_audit`'i adlandırıyor ve henüz `tenant_billing`'i adlandırmıyor. goose'un sürüm
    tablosu **okunmaz**: testler Up'ı HEAD'de, işlem içinde yeniden koşar.
13. **Başka görevlerin testlerinde güncellemeler (zayıflatılmadı):**
    `TestOperatorSQL_OnlyBoundParameters` 14 → 15 sabit/çağrı;
    `TestOperatorAccessors_TheCustomerRoleCannotUseThem` + `TenantBilling`;
    `TestOperator00026_PrivilegeMatrix` izin listesi (`tenants`, `employees` genişledi,
    `billing_periods` yeni — tam sütun listeleriyle); `opDefinerForeignExecFindings` (md. 10;
    `TestOperator00030_TheDefinerExecutesOnlyItsOwnAndPublicFunctions` ve
    `TestOperator00031_TheDefinerReadsTheLogAndTheOperatorDoesNot` onu kullanır);
    `TestOperator00031_TheFunctionsAndTheirExactSignatures`'ın bilet tür pini *"00031'in beş
    türünü tutan kapalı küme"* oldu (00030'un pininin biçimi; HEAD'deki tam küme
    `TestOperator00032_TheFunctionsAndTheirExactSignatures`'da, 00031'in tam kümesi 00032'nin
    Down'ından sonra `TestOperator00032_DownGivesBack00031AndUpTakesItAgain`'de);
    `TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList` (yeni türün sevk edilen `detail`'i,
    bir tanınan ve bir tanınmayan şekil); `TestOpReadAudit_AForgedTicketIsRefused` (okuma
    hash'i beşinci bir türün altında da). `TestOperator00031_DownGivesBack00030AndUpTakesItAgain`
    00031'e `opAtVersion` ile — önce 00032'nin Down'ını koşarak — iner; değişiklik gerekmedi.
14. **2. tur (2026-10-07, üçüncü gözün RED'i: bir test boşluğu; SQL ve Go davranışı
    değişmedi).** Okuma iki ayı tenant'ın **zone'unda** alır: kayıt ayı (`signup_month`) ve
    içinde bulunulan ay (`this_month`). Çapraz testin fikstürleri kaydı hep yerel ayın 15'i
    12:00'ye koyuyordu ve testin öncülü *"başka bir takvim günü"* soruyordu, *"başka bir ay"*
    değil: ikisini UTC'de alan mutasyonlar (M2, M1) yeşil kalıyordu (denetçi ölçtü; M1 yalnız bir
    ay sınırının ≈26 saatlik penceresinde kırmızı olabilir). Kapatılan:
    - **M2 — kayıt ayı:** çapraz teste iki uzak zone'da birer **kenar** tenant'ı eklendi:
      Etc/GMT-14'te yerel ayın 1'i 00:30 (UTC'de önceki ay), Etc/GMT+12'de son günü 23:30
      (UTC'de sonraki ay); öncül SQL'de ölçülür (kayıt anının UTC ayı yerel kayıt ayı değil). Her
      ay okuma ve tenant'ın yolu karşılaştırılır; ayrıca okumanın `after_signup`'ı yerel kayıt
      ayından itibaren doğru olmalı. **Ölçüldü:** kayıt ayını UTC'de alan mutasyon (M2) çapraz
      testi kırmızıya çevirir.
    - **M1 — içinde bulunulan ay, saatten bağımsız:** seçenekler (a) gerçek saatle karşılaştırma
      — saat oynatılamadığı için ayın 1'i ya da son günü değilse fark çıkmaz, yani tek başına
      saatten bağımsız bir kırmızı değildir; (b) gövde metninde `AT TIME ZONE` pini — mutasyonu
      metinden yakalar, davranışı ölçmez; (c) sayılı sınır. **Seçilen: (a), saati okumanın
      sorgusunda oynatarak.** Yeni test `TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant`
      okumanın `RETURN QUERY`'sini **katalogdan** (işlemin içindeki `pg_proc.prosrc`) alır, iki
      PL/pgSQL adını (`p_tenant_id`, `v_page`) ve her `clock_timestamp()`'i parametreye bağlar ve
      `tappa_opdefiner` olarak, fonksiyonun `search_path`'iyle koşar. KONTROL: üç zone'daki
      tenant'ın gerçek okuması, okumanın hemen önceki ya da hemen sonraki anındaki ikame sorgunun
      satırlarına metin metin eşit. Sonra iki seçilmiş an: içinde bulunulan UTC ayının
      başlamasından **4 saat önce** (Etc/GMT-14 o aya girmiş, UTC girmemiş) ve **5 saat sonra**
      (Etc/GMT+12 hâlâ önceki ayda) — iki öncül de SQL'de ölçülür — her tenant için on iki ay,
      testin kendi `date_trunc('month', an AT TIME ZONE zone)` hesabından başlar. Kapalı hata:
      ikamenin oynatmadığı bir saat (`now()`, `statement_timestamp()`, `CURRENT_DATE`…) ya da
      bağlamadığı bir PL/pgSQL adı (ay `RETURN QUERY`'den önce bir değişkene hesaplanırsa) testi
      geçirmez, düşürür. Sınırı yöntemindir: ölçülen, sorgu metninin bir andaki davranışıdır ve
      fonksiyonun dışında koşar; ikisini gerçek anda KONTROL bağlar. (b) eklenmedi: davranış
      testi kırmızıyken metin pini ikinci bir kopyadır. **Ölçüldü:** içinde bulunulan ayı UTC'de
      alan mutasyon (M1) yeni testi kırmızıya çevirir; aynı mutasyon çapraz testle tek başına
      bugün (2026-10-07, bir ay sınırından uzak) **YEŞİL** kalır (M1g — kapatılan boşluğun
      kendisi). L13 gerekmedi.
    - Üç evrensel cümle daraltıldı (00032'nin başlık yorumu, bu notun md. 2'si, `billing.sql`'in
      son cümlesi) ve `internal/db/operator.go`'nun iki yorumu: *"held equal … month by month"*
      artık *"fikstürlerinde karşılaştırılır"* der; `MoneyFromNumeric` için *"refuses any scale
      but 2"* yanlıştı — ikiden fazlasını reddeder, azını kabul eder (L4).
    - md. 4'teki tip değiştiricisi olgusu bu turda ölçüldü (komutuyla); md. 11'deki *"Down
      doğrulanmış dala girdi"* cümlesi ölçülmemişti, çıkarıldı.
15. **3. tur (2026-10-07, güvenlik denetimi ONAY ve bir DÜŞÜK bulgu; yalnız test — SQL, Go ürün
    kodu ve migration değişmedi).** *"Argüman hata alanına dönmez"* denetimini yapan iki yardımcı
    — `opWantClean` (`internal/db/operatorlegal_test.go`) ve `TestOperator00026_ArgumentsNeverComeBackInAnError`'ın
    kendi `clean`'i (`internal/db/operatorfuncs_test.go`) — yalnız Message, Detail, Hint, Where,
    ConstraintName ve ColumnName'e bakıyordu; denetçinin S12b mutasyonu (`op_begin_read`'in sayfa
    sınırı reddine `TABLE = p_session`) bu yüzden yeşil kalıyordu, oysa değer istemciye `TABLE NAME`
    alanı olarak gider. İki listeye `TableName`, `SchemaName` ve `DataTypeName` eklendi: artık
    `RAISE … USING`'in doldurabildiği her alan (MESSAGE, DETAIL, HINT, COLUMN, CONSTRAINT,
    DATATYPE, TABLE, SCHEMA) ve bağlam satırı aranır; yardımcıların başka davranışı değişmedi.
    Ölçüldü: S12b ve S12 (`op_read_tenant_billing`'in reddine `TABLE = p_ticket`) **KIRMIZI**;
    yalnız alan denetimi yapan testlere karşı aynı iki mutasyon genişletilmiş listeyle KIRMIZI
    (S12b-f, S12-f), 2. turun dar listesiyle **YEŞİL** (S12b-pre, S12-pre — kapatılan boşluğun
    kendisi). Öteki testler bu mutantları gövdeden de görür (Down testinin kimlik öncülü, tüketim
    biçimi taraması, `NULL` biletle `TABLE` seçeneğinin `NULL` olması — 22004). Genişleme başka hiçbir
    testte kırmızı çıkarmadı (`internal/db` 341 PASS; tek kırmızı L9/00027). `InternalQuery` gibi
    `RAISE`'in dolduramadığı alanlara bakılmaz. `tappa_operator`'ın `op_*` dışı EXECUTE kümesini
    tutan bir pinin yokluğu (denetçinin S16'sı; 00032'den önce de vardı) bu kartın değil,
    orkestratörün backlog'undadır.

**Sayılı sınırlar (OP-12 A):**
- **L1** — Tanımlayıcı `employees.activated_at`/`deactivated_at`'i okur (billable yükleminin
  argümanları); okuma onları döndürmez, yalnız sayar (sınır 9'un kuralı). Bir gövde değişikliği
  onları döndürebilir — imza testi dönüş tipini pinler, gövdeyi değil.
- **L2** — Go'daki satır-tenant denetimi (`errBillingOfAnotherTenant`) tek başına ulaşılamaz bir
  daldır: SQL'in `t.id = p_tenant_id`'si önce cevap verir (OP-13'ün L2'si).
- **L3** — Ön koşulun sunucu sürümü dalı bu kümede (17) sürülemez; testi metnini pinler.
- **L4** — **pgx sıfırın ölçeğini taşımaz:** rakamı olmayan bir `numeric` (0,00) Go'ya
  `0 × 10^0` olarak gelir (`pgtype` v5.10.0, `numeric.go`, ikili çözme — okundu). Go tarafında
  ölçek yalnız sıfır olmayan tutarlarda görünür (`Exp = −2`); 0,00'ın ölçeğini yalnız SQL'deki
  `scale()` ölçer (`TestOpReadTenantBilling_MoneyIsNumericAtScaleTwo`). `billing.MoneyFromNumeric`
  ikiden **fazla** ondalığı reddeder, azını kabul eder — ölçeği düşmüş bir tutar Go'dan sessizce
  geçerdi; bariyer SQL testidir.
- **L5** — **Maliyet, gözlem** (2026-10-07, geliştirme veritabanı, `tappa_owner`, salt-okuma
  işlemde, okumanın eşdeğer `SELECT`'i; seçilen tenant: `SELECT tenant_id FROM employees GROUP BY 1
  ORDER BY count(*) DESC LIMIT 1` — test kalıntısı, boyu yazılmaz). On iki canlı ay: ay başına alt
  sorgu biçimi 6,3–8,3 sn (üç koşu), tek tarama + gruplama 6,1–8,1 sn (üç koşu), sınırlar ve
  kadro `MATERIALIZED` — sevk edilen biçim — 4,6–6,1 sn (beş koşu); koşular dönüşümlü, paylaşılan
  veritabanı gürültülü. **Biçim maliyeti anlamlı düşürmüyor:** aynı yüklem satıra yazıldığında
  (yalnız ölçüm, sevk edilmedi) aynı sorgu 0,10–0,11 sn — süre yardımcı çağrılarındadır. 00016'nın yorumu *"tek ifadelik IMMUTABLE bir SQL fonksiyonu
  planlayıcı tarafından satır içine alınır"* der; beş fonksiyonun `SET search_path`'i
  (`proconfig`) bunu engeller — PostgreSQL `SET` cümleli bir fonksiyonu satır içine almaz
  (planda çağrı olarak görülür; ölçüldü). 🔴 **00016'nın bölüm 2 yorumu bu yüzden yanlıştır**
  (*"A SQL function that is IMMUTABLE and a single expression is inlined by the planner, so the
  sharing costs nothing at run time"*): aynı sayfa yardımcılarla 4,6–6,1 sn, yüklem satıra
  yazılınca 0,10–0,11 sn (yukarıdaki ölçüm). Bu,
  tenant'ın kendi önizlemesini de etkiler. 00016 uygulanmış bir migration'dır, yorumuna
  dokunulmadı; düzeltmesi (yardımcıların tanımı, yeni bir migration) bu görevin işi değildir ve
  orkestratörün backlog'una gider. Üretimde kadrolar onlarla ölçülür. **Üçüncü gözün ölçümü**
  (2026-10-07, ayrı koşular, aynı tenant değil): sevk edilen biçim 2,2–2,5 sn, yüklem satıra
  yazılınca 0,07–0,09 sn — oran (≈25–35×) yukarıdakiyle tutarlı; `EXPLAIN VERBOSE`'ta
  yardımcılar çağrı olarak durur. 🔴 **B fazına devir:** operatör havuzunda `statement_timeout`
  yoktur; okuma işlemi kendi `SET LOCAL statement_timeout`'unu koymalı ve zaman aşımı (57014)
  başka her veritabanı hatası gibi 503 yoluna düşmeli — asla 0,00 basan bir sayfa değil.
- **L6** — Tanınmayan bir zone'u kayıtlı tenant'ın okuması, okuma satırı commit edildikten sonra
  22023 ile düşer (`time zone … not recognized`); tenant'ın kendi önizlemesi de aynı yardımcıda
  düşer (salt-okuma ile ölçüldü; testi `TestOpReadTenantBilling_AnUnknownZoneIsAnErrorNotAZeroInvoice`).
  Şemada zone'u reddeden bir kısıt yoktur (M7-05 yazımı Go'da doğrular) ve geliştirme
  veritabanında böyle bir kalıntı tenant vardır. Sıfır fatura değil, hatadır (B: problem sayfası).
- **L7** — Dondurulmuş bir ayın `after_signup` ve `period_has_ended`'i sabit doğrudur
  (`frozenDraft`'ın kuralı): sahibin elle, bitmemiş bir ay için yazdığı satır da "bitti" okunur
  — sınır 5'in sahibi.
- **L8** — Sınır 4'ün penceresi 30 sn (geri alınan okuma bileti yeniden okunur).
- **L9** — **sqlc, `billing.sql`'deki her yorumu `internal/store`'a kopyalar** — başlıktakini ilk
  sorgunun belgesi olarak (ölçüldü: başlığa eklenen bir cümle `billing.sql.go`'yu değiştirdi;
  son ifadeden sonraki bir cümle değiştirmedi). Kart taslağının T10'u (*"yalnız yorum eklenirse
  `internal/store` değişmez"*) başlık için yanlıştı; cümle sona yazıldı.
- **L10** — Donan saat taraması yardımcıların **içini** görmez (§6); beşinin kaynağı bugün saat
  okumaz (salt-okuma ile okundu: hiçbirinde yasak adların hiçbiri yok) — okundu, pinlenmedi.
- **L11** — Down ve ön koşul mutasyonları goose döngüsüyle değil, testlerin dosyanın kendi
  bölümlerini koştuğu geri alınan işlemlerde ölçülür (OP-13'ün L10'u).
- **L12** — Commit eden iki test koşu başına **+4** `read` satırı (yaşam döngüsü 1, havuz 3 —
  veritabanında çalışansız bir tenant yoksa havuz 1), **+2** `platform_admins` (`disabled`) ve
  **+2** iptal edilmiş oturum bırakır; bilet 0; tenant verisi commit edilmez. Ölçüldü (2026-10-07;
  veritabanı saatinden T0, yalnız bu iki testin tek koşusu, satırlar `target_scope =
  'tenant_billing'` üzerinden atfedilerek — o kapsamı yalnız bu kartın kodu yazar): 4 okuma
  satırı, 2 aktör (ikisi `disabled`), 2 oturum (ikisi iptal), bilet 0, `op12` adlı commit edilmiş
  tenant 0, onların `billing_periods` satırı 0.

**Güvenlik iddiası — üç parça.**

- **Tehdit modeli:** Bu pinler kazara sapmaya karşıdır; bir pini bilerek atlatmak kod
  incelemesinin konusudur.
- **PART I — bugün sevk edilen kodun ölçülen davranışı** (test · girdiler · assert; 2026-10-07,
  `.env` yüklü `-race` koşusunda yeşil; onları kırmızıya çeviren mutasyonlar OP-12 A kart
  düzeltmesinin tablosunda — 2. turda sayılan 52'nin 52'si istenen sonucu verdi: 49 kırmızı, 3
  tasarım gereği yeşil — X0, açığın kendisi; G5/L2; M1g, 2. turun kapattığı boşluğun kendisi):
  - `TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure` · üç zone (Europe/Malta, Etc/GMT-14,
    Etc/GMT+12) × iki tenant (zone başına üç okuma) ve iki uzak zone'da birer **kenar** tenant'ı
    (kayıt anı UTC'de başka bir ayda — 2. tur), 132 ay; sınırda ve bir mikrosaniye sonra ayrılan, sınırda
    ve bir mikrosaniye önce katılan, damgası durumuyla çelişen, davetli; founding'in son ücretsiz
    ve ilk ücretli ayı ürünün kendi ifadesiyle kapatılmış, kapamadan sonra kadro ve fiyat
    değişmiş; standard tenant'ın ikinci sayfası kaydın öncesine uzanır · her ay tenant'ın kendi
    yoluyla alan alan eşit, para tam eşit ve SQL'de ölçek 2; her ay türü her zone'da var; sınır
    ayı tam dört sayar; dondurulmuş ücretli ay bugünün önizlemesinden hem sayıda hem fiyatta ayrı;
    kenar tenant'larının `after_signup`'ı yerel kayıt ayından itibaren doğru (öncül: kayıt anının
    UTC ayı yerel kayıt ayı değil); bir uzak zone UTC'den başka bir takvim **gününde** (gün, ay
    değil — içinde bulunulan ayın zone'u bir sonraki maddenin işidir).
  - `TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant` (2. tur) · okumanın
    `RETURN QUERY`'si katalogdan, `p_tenant_id`/`v_page`/`clock_timestamp()` parametreye bağlı,
    `tappa_opdefiner` olarak; üç zone'da birer tenant · KONTROL: gerçek okuma, okumanın iki
    yanındaki anlardan birindeki ikame sorguya metin metin eşit; UTC ayının başlamasından 4 saat
    önce ve 5 saat sonra (öncüller: Etc/GMT-14 ve Etc/GMT+12 o anlarda UTC'den başka ayda) her
    tenant'ın on iki ayı kendi zone'undaki aydan başlar; ikamenin oynatmadığı saat ya da
    bağlamadığı değişken testi düşürür.
  - `TestOpReadTenantBilling_ReturnsOnlyTheNamedTenantsFigures` · aynı ayı kapatılmış iki tenant ·
    her biri yalnız kendi id'si, adı, tek dondurulmuş ayı ve kadrosu.
  - `TestOpReadTenantBilling_AnUnknownTenantReadsNothingAndAKnownOneTwelveMonths`,
    `TestOpReadTenantBilling_AnUnknownZoneIsAnErrorNotAZeroInvoice`,
    `TestOpReadTenantBilling_MoneyIsNumericAtScaleTwo`, `TestOpReadTenantBilling_PagesAreBoundedInTheBody`.
  - `TestOperator00032_TheFunctionsAndTheirExactSignatures` · katalog · tam imza ve sonuç
    (`closed_by` yok; float yok, iki para sütunu `numeric`), sahip, `proconfig`, overload,
    EXECUTE yalnız `tappa_operator`; taramalar bulgusuz; bilet CHECK'i HEAD'de tam altı tür.
  - `TestOperator00032_TheDefinerReadsBillingAndTheOperatorDoesNot` · tanımlayıcının üç tablodaki
    tam listeleri, `closed_by`/ad/adres/yazma 42501; `tappa_operator` doğrudan SELECT ve `COPY`
    42501; `tappa_app` okuması 42501, beş fonksiyondaki EXECUTE'u duruyor.
  - `TestOperator00032_TheDefinerExecutesExactlyTheFiveBillingHelpers` · md. 10.
  - `TestOperator00032_DownGivesBack00031AndUpTakesItAgain` · md. 11; Down'ın iki gövdesi 00031'in
    dosyasıyla birebir; `REVOKE ALL` yok; tuzak: genel bakış Down'dan sonra okur.
  - `TestOperator00032_PreconditionRefusesAWrongCluster`, `TestOperator00032_CallersTempTableIsNeverRead`.
  - `TestOpBeginRead_TheBillingKindBindsTheTenantAndAPage`,
    `TestOpReadTenantBilling_ATicketFromThisTransactionIsRefused`,
    `TestOpReadTenantBilling_AForgedTicketIsRefused`, `TestOpReadTenantBilling_RefusesEveryDeadSession`,
    `TestOpReadTenantBilling_ExpiryIsTheWallClock` · §2 v'nin iki aşama tablosu bu okuma için.
  - `TestOpReadTenantBilling_TwoPhaseLifecycle`, `TestTenantBilling_OnThePoolTheTwoPhasesAreTwoTransactions` ·
    gerçek commit'lerle, üretim kurucusunun havuzunda.
- **PART II — adıyla pinler:** `TestOperator00032_TheFunctionsAndTheirExactSignatures` (imza,
  sonuç tipleri, CHECK); `TestOperator00032_DownGivesBack00031AndUpTakesItAgain` (iki `NOT VALID`
  koşulu bütün `WHERE` cümlesiyle, Down'ın iki gövdesi, `REVOKE ALL`'ın yokluğu);
  `TestOperator00032_TheDefinerExecutesExactlyTheFiveBillingHelpers` ve
  `opDefinerForeignExecFindings`'in üç kuralı (tanımlayıcının `op_*` dışı EXECUTE kümesi = adlı
  beş); `TestOperator00026_PrivilegeMatrix` (genişletildi); `TestOpRead_EveryReadConsumesItsTicketAsTheADRSays`
  (yeni okumanın tüketen `UPDATE`'i); `TestOperator00026_NoFrozenClock` (yeni gövde ve iki
  değiştirilen); `TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList` ("every read kind");
  `TestOperatorSQL_OnlyBoundParameters` (on beş sabit ve çağrıları);
  `TestTenantBilling_FivePagesOfTwelveAreTheTenantsHistoryCap` (sayfa sınırı × ay = `HistoryCap`);
  `TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant` (içinde bulunulan ayın zone'u,
  saatten bağımsız; 2. tur).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

## OP-14 D uygulama notu (2026-10-07, migration 00033 — K14-2)

Uygulama: `db/migrations/00033_audit_opadmin_actions.sql` · `cmd/opadmin/main.go` (`auditRow`,
üç tür sabiti; başlığın PART I/II'si) · `internal/db/operator.go` (yalnız kendi bloğu: üç
`OperatorAuditKind` sabiti ve `ByOwner`; `operatorAuditKinds` dizisine üç ad) ·
`internal/handler/operator/audit.go` (üç sözcük, `ByOwner`) · `web/templates/operatorpages`
(`AuditRow.ByOwner`, `audit.templ`'de bir dal, yeniden üretilen `audit_templ.go`). Yeni testler:
`internal/db/operatorowneraudit_test.go`, `cmd/opadmin/audit_test.go`,
`cmd/opadmin/audit_db_test.go`, `internal/handler/operator/op14d_test.go`; güncellenen pinler md.
9'da. Yeni bağımlılık yok. Uygulamanın karar verdiği yerler, adıyla:

1. **Türler:** `operator_created`, `operator_mfa_reset`, `operator_disabled` — günlüğün bir hesap
   hakkındaki olgu kalıbı (`login_failed`, `enrollment_failed`: şey, sonra başına gelen). CHECK
   sırasında `legal_publish`'ten sonra; Go sabitleri `OperatorAuditOperatorCreated`,
   `OperatorAuditOperatorMFAReset`, `OperatorAuditOperatorDisabled`.
2. **`actor_shape`'in üçüncü kolu (taslağın C4'ü):** `kind IN (üç sahip türü) AND session_id IS
   NULL AND actor_admin_id IS NULL AND target_admin_id IS NOT NULL AND target_tenant_id IS NULL
   AND target_scope IS NULL AND page_number IS NULL AND page_size IS NULL AND detail =
   '{}'::jsonb`; oturum kolu dokuz türü (altı oturum öncesi + üç sahip) dışlar. İki kısıt
   adlarıyla ve birlikte değişti (00027/00031 deseni). Kol satırın **bütün** şeklidir: satır
   hesabı **id'siyle** adlandırır ve başka hiçbir şey taşımaz; `detail`'e adres ya da ad yazan
   bir opadmin düzenlemesi testte değil **şemada** reddedilir (23514).
3. **Yazıcı:** opadmin'in SQL'i satırı hesabın işini yapan aynı `DO` bloğunun iç bloğunda,
   hesabın son yazısından **sonra** ve kısıt yakalayıcısından **önce** yazar:
   `INSERT INTO public.operator_audit_log (kind, target_admin_id) VALUES ('<tür>', '<id>'::uuid);`.
   İş ve iz tek ifadedir — birlikte commit edilir ya da hiçbiri. İfade metnine giren yalnız tür ve
   hesap id'sidir (id zaten oradaydı); COPY veri satırı, yedi ifadelik zarf, `NOTICE`, üretim zamanı
   korumaları ve A-B-A koruması değişmedi. Satırı reddeden bir kısıt opadmin'in yakalayıcısıyla
   adıyla döner, `DETAIL`'siz.
4. **`at` — iki katman (taslağın C3'ü; OP-14 notu md. 7'nin açık bıraktığı):** (a) INSERT'in sütun
   listesinde `at` yok, pinli; (b) `BEFORE INSERT … FOR EACH ROW` tetikleyicisi
   `operator_audit_log_at_is_the_wall_clock` → `tappa_audit_at_is_the_wall_clock()`: sahibin
   (migration rolünün) fonksiyonu, SECURITY INVOKER, `search_path = pg_catalog, pg_temp`, gövde
   koşulsuz `NEW.at := pg_catalog.clock_timestamp(); RETURN NEW;`. Sahibi de bağlar — `INSERT`,
   `INSERT … SELECT`, `COPY` — tablonun sahibi onu kapatana, düşürene ya da fonksiyonunu
   değiştirene dek (LD2; sınır 5). **Append-only tetikleyicileriyle
   sıra:** bu tetikleyici yalnız INSERT'te ateşlenir; `operator_audit_log_append_only` `BEFORE
   DELETE OR UPDATE … FOR EACH ROW`, `operator_audit_log_no_truncate` `BEFORE TRUNCATE … FOR EACH
   STATEMENT`tir (salt-okur katalog ölçümü, 2026-10-07) — hiçbir olayda ikisi birlikte ateşlenmez,
   sıraları anlamsızdır; `at`'in UPDATE'i append-only tetikleyicisinindir (23001). **Tanımlayıcının
   satırı anlamca değişmez:** `at`'in DEFAULT'u `clock_timestamp()` idi, şimdi aynı INSERT'te bir an
   sonra okunan aynı saattir. PUBLIC'in fonksiyondaki EXECUTE'u **bırakıldı** — şemadaki sekiz
   tetikleyici fonksiyonunun sekizi gibi (salt-okur ölçüm, 2026-10-07: `proacl` boş, `tappa_app`
   ve tanımlayıcı EXECUTE edebilir); tetikleyici fonksiyonu tetikleyici dışında çağrılamaz ve bir
   tabloya bağlamak (`CREATE TRIGGER`) o tabloda **TRIGGER yetkisi** ister — yetki, sahiplik değil
   (PostgreSQL belgesi) — ve `tappa_app`, `tappa_operator`, tanımlayıcı ve resolver'ın hiçbir
   operatör tablosunda bu yetkisi yok (`TestOperator00026_PrivilegeMatrix`'in tablo düzeyi
   fiilleri; denetçinin ölçümü: üç rolün `operator_audit_log`'daki `CREATE TRIGGER`'ı 42501
   *"permission denied for table"*). 00011'in notu iki reddi `tappa_app` için ölçtü; parantezi
   sahipliği adlandırır, koşul yetkidir. *(3. tur düzeltmesi: ilk yazım "tablonun sahipliği"
   diyordu.)* Tanımlayıcının
   `op_*` dışı EXECUTE taraması (00032'nin üç kuralı) bunu bulgu saymaz: PUBLIC'in çağırabildiği,
   SECURITY DEFINER olmayan bir fonksiyon. **Aşama 2'de ölçüldü (orkestratörün kararının iki
   dayanağı, `TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger`):** (a) doğrudan çağrı
   kimden gelirse gelsin — sahip, `tappa_app`, `tappa_operator`, tanımlayıcı — PostgreSQL'in
   kendisince reddedilir: SQLSTATE `0A000`, *"trigger functions can only be called as triggers"*;
   (b) tetiklenme anında EXECUTE **denetlenmez**: PUBLIC'in EXECUTE'u bir savepoint'te geri
   alınınca tanımlayıcı fonksiyonu çağıramaz (`has_function_privilege` false, doğrudan çağrı
   42501), ama `op_record_auth_event`'in — tanımlayıcı olarak koşan — INSERT'i onu yine ateşler
   (aynı savepoint'te bir işaret tarih basan gövde, satırda o tarihi bırakır; kontrol: savepoint'ten
   sonra satır saatle tarihlenir).
5. **Tür kümesinin kopyaları:** audit tür CHECK'i (on dört), `op_begin_read`'in filtre listesi,
   `op_read_audit`'in filtre şekli ve Go'nun `OperatorAuditKinds`'ı birlikte genişledi (OP-14 notu
   md. 5'in pini); `actor_shape`'in sahip kolu = `OperatorAuditKind.ByOwner`; opadmin'in üç literali
   (sürücüsüz ikili `internal/db`'yi içe aktaramaz) `ByOwner`'a testten bağlı; görüntüleyicinin
   sözlüğü tür sabitlerine `go/types` ile bağlı. `op_begin_read` ve `op_read_audit` 00032'nin
   gövdeleri + birer listedir (diff: −1 +2 ve −1 +3 satır; test düzenlemeyi geri alınca 00032'nin
   gövdesini bayt bayt ister). `op_read_audit`'in şekil listesine satır **gerekmedi**: sahip
   satırının kapsamı NULL, `detail`'i `{}` — listenin son satırı zaten tanır (L3'ün *"yeni bir
   türün çıplak satırı tanınır"* hâli); değişen yalnız filtre şeklidir, yoksa sahip türüne süzülmüş
   bir okumanın satırı *"detail not shown"* okunurdu.
6. **Görüntüleyici:** üç sözcük (*"Operator account created"*, *"Operator account reset to a new
   setup link"*, *"Operator account disabled"*) ve `AuditRow.ByOwner`: sahip satırı *"Before
   sign-in"* değil *"By the platform owner, with opadmin"* der. **Orkestratörün listesinin dışında,
   gerekçesiyle:** yalnız sözcük eklenseydi satır *"Operator account created · Before sign-in"*
   okunurdu — denetimin tek ekranında yanlış bir olgu (satır oturum öncesi değil, oturum dışıdır).
   Bedel: `view.go`'ya bir `bool` alan, `audit.templ`'e bir dal, alan listesi pini
   (`TestAuditScreen_NoCredentialFieldReachesThePage`) o alanla genişledi; ekran sahip türünü
   `internal/db`'nin `ByOwner`'ından okur, kopya tutmaz.
7. **Down:** iki fonksiyon 00032'nin Up gövdelerine bayt bayt döner (salt-okur ölçüm, 2026-10-07:
   canlı `op_begin_read` 9311 karakter/9312 bayt ve `op_read_audit` 5404 karakter, md5'leri 00032
   Up'ınkine ve 00033 Down'ınkine eşit), tetikleyici ve fonksiyonu düşer (sahip yeniden `at`
   seçebilir), CHECK'ler 00031'in kümelerine döner — 00031'in on bir türü dışında bir satır varsa
   ikisi birlikte `NOT VALID`, yoksa doğrulanmış. Up aynı soruyu kendi on dört türüyle sorar
   (OP-13 notunun L11'i: Down/Up zinciri bileşir). Down'da `REVOKE`, `GRANT`, `DISABLE TRIGGER`
   yok — Up yetki değiştirmez.
8. **Ön koşul:** 00026'nın rol denetimleri (00027–00032 gibi) ve 00032'nin izleri:
   `op_read_audit` ve `op_begin_read` tanımlayıcının, `op_read_tenant_billing` var, audit tür
   CHECK'i `password_ok`'u adlandırır ve `operator_created`'ı henüz adlandırmaz, `actor_shape` var.
9. **Başka görevlerin testlerinde güncellemeler — neden (zayıflatılmadı):** (a) `opAuditKinds`
   HEAD'in on dördüdür, 00031'in on biri `opAuditKinds31` (00031'in Down testi — `opAtVersion` önce
   00033'ün Down'ını koşar); (b) `TestOperator00031_TheFunctionsAndTheirExactSignatures` audit
   CHECK'leri için *"00031'in on birini tutan kapalı küme, ilk kolu altı oturum öncesi tür"* —
   00030/00031'in bilet pininin biçimi; HEAD'in tam metni `TestOperator00033_TheKindsTheShapeAndTheClock`'ta;
   (c) `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree` üç kolu ve `ByOwner`'ı ister,
   `op_record_auth_event`'in sahip türlerini reddettiğini sayar; (d) `opLogInsert` sahibin
   tarihlediği bir satırdan önce tetikleyiciyi **testin geri alınan işleminde** kapatır
   (`opOwnerDatesRows`; `TestOpReadAudit_PagesAreCappedOrderedAndBounded` de) — sıralı fikstürler
   ve `at` eşitliği (`TestOpReadAudit_NewestFirstAndTheViewersOwnRowLeads`'in kolu) başka türlü
   kurulamaz. **Orkestratörün koşulu:** yalnız geri alınan işlemde ve operatör tabloları danışma
   kilidi **EXCLUSIVE** tutulurken — `opMustHoldTheTablesLockExclusive` aksi hâlde testi düşürür
   (`opAtVersion`'ın silme dalı da). **Ölçüldü (Aşama 2):** `DISABLE TRIGGER` tabloda
   `ShareRowExclusiveLock` alır (ACCESS EXCLUSIVE değil; öncesinde oturumun tuttuğu
   `AccessShare`/`RowShare`/`RowExclusive` dışında yeni tek kip) ve başka bir oturumun INSERT'i
   ona takılır (300 ms `lock_timeout` → 55P03; o oturumun işlemi geri alındı) —
   `TestOperator00033_TheRowsTimeIsTheWallClockWhoeverWritesIt`. Kilidin tutulduğu süre (devre
   dışı bırakmadan geri almaya kadar, `OP14D-LOCK` log satırı) `internal/db`'nin tam `-race`
   koşusunda altı olayda **69–218 ms**; (e) `opAtVersion` 33'ün altına inerken sahip türlü satırları işlem içinde siler
   (biletler için olan kuralın audit karşılığı; geliştirme veritabanında bugün 0 satır); (f)
   `TestAuditWords_NameEveryKindAndNothingElse`'in CONTROL'ü 11 → 14 ve alan listesi pini; (g)
   `TestApply_AFailureAnywhereLeavesNoRow` audit satırlarını da sayar ve **T94'ü kapatır**: rapor
   alt testi betiği artık veritabanının saatiyle üretir.

**Sayılı sınırlar (OP-14 D):**
- **LD1** — Satır *"sahip uyguladı"* der, **hangi insanın** uyguladığını değil: sahip tek bir
  veritabanı rolüdür; psql oturumu kayıt dışıdır.
- **LD2** — Sahip (`tappa_owner`, tablonun sahibi; dağıtılan topolojide ayrıca süper kullanıcı)
  betikten satırı silip uygulayabilir; tarih seçmek için tetikleyiciyi kapatabilir (`ALTER TABLE …
  DISABLE TRIGGER` tablonun **sahipliğini** ister, süper kullanıcılığı değil — PostgreSQL belgesi),
  düşürebilir ya da
  fonksiyonunu `CREATE OR REPLACE` ile değiştirebilir (`TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger`
  bunu bir savepoint'te yapar); süper kullanıcının `session_replication_role = replica` oturumu
  da tetikleyiciyi atlar (PostgreSQL belgesi; ölçülmedi — ajan kuralı) — sınır 5. *(2. tur düzeltmesi: ilk yazım
  yalnız `DISABLE`'ı ve süper kullanıcıyı sayıyordu.)* İz sahibe karşı bir kontrol değil, ürettiği betiği
  değiştirmeden uygulayan sahibin eylemlerinin kaydıdır.
- **LD3** — Tanımlayıcı rolün 00026'dan beri tuttuğu INSERT sütunları bir sahip satırına yeter:
  tanımlayıcı **olarak** koşan bir ifade sahip satırı yazabilir (sayılı kol:
  `TestOperator00033_TheOwnerArmIsExactlyAnAccountAndNothingElse`). Tanımlayıcı olarak yalnız `op_*`
  gövdeleri koşar ve hiçbiri iki filtre listesi dışında bir sahip türü adlandırmaz (katalog pini,
  `TestOperator00033_TheKindsTheShapeAndTheClock`); `op_record_auth_event` onları reddeder.
- **LD4** — Up'ın `NOT VALID` sorusu türdür (00031'in kuralı): sonraki bir migration üçüncü kolun
  şeklini değiştirip Down'ında başka şekilli bir sahip satırı bırakırsa bu dosyanın doğrulanmış
  yeniden eklemesi 23514 ile düşer. Böyle bir migration yok.
- **LD5** — 00033'ten önceki eylemlerin satırı yoktur; append-only günlük geriye doldurulmaz
  (README O9-1'in eski izi o eylemler için geçerli kalır).
- **LD6** — 00033'ü görmemiş bir veritabanında bu değişikliğin opadmin betiği
  `operator_audit_log_actor_shape` ile (23514; CHECK'ler ad sırasıyla koşar, sahip türü 00031'in
  oturum koluna düşer) reddedilir ve hiçbir şey değişmez — sıra önce deploy, sonra opadmin (README).
  Ölçüldü (Aşama 2): Down'dan sonra opadmin'in INSERT'i `operator_audit_log_actor_shape` adıyla
  23514 (`TestOperator00033_DownGivesBack00032AndUpTakesItAgain`); ad sırası ayrıca: hem
  `actor_shape`'i hem tür CHECK'ini bozan satır `actor_shape`, hem tür CHECK'ini hem
  `target_scope`'unkini bozan satır tür CHECK'i adıyla döner
  (`TestOperator00033_TheOwnerArmIsExactlyAnAccountAndNothingElse`).
- **LD7** — Bir sahip satırı varken 00027'nin uygulanmış Up'ı yeniden koşturulamaz (L9'un sınıfı).
- **LD8** — Ekran sahip satırını **türünden** tanır (`ByOwner`): bu yapının adlandırmadığı
  oturumsuz bir tür bugünkü gibi *"Before sign-in"* der (OP-14 B'nin dalı) — o tür sözlükte de
  *"Unrecognised"* çizilir. *(2. tur: `operator_` önekli ama adlandırılmamış bir tür —
  `operator_enabled` — de sahibin sayılmaz; `ByOwner` bir önek kuralı değil, kapalı küme:
  `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree`, `TestAuditScreen_TheOwnersRowsNameTheOwner`.)*
- **LD9** *(2. tur, üçüncü gözün bulgusu)* — **opadmin'in yarattığı bir hesap artık silinemez:**
  her `create` bir `operator_created` satırı yazar; satır hesabı `target_admin_id` yabancı
  anahtarıyla (`ON DELETE RESTRICT`) tutar ve günlük append-only'dir. Ölçüldü: yaratılan hesabın
  sahip `DELETE`'i 23503 (`operator_audit_log_target_admin_id_fkey`; kontrol: hiçbir satırın
  adlandırmadığı hesap silinir) — `TestAudit_EachActionLeavesExactlyOneRowAndARefusalNone`.
  00033'ten önce kullanılmamış, yanlış girilmiş bir `pending` hesap silinebiliyordu. Düzeltme yolu:
  `disable` + doğru adresle yeni bir `create`; yanlış hesap `disabled` ve adresiyle kayıtlı kalır.
  Bunun bedeli de ölçüldü (aynı test): devre dışı hesabın adresiyle yeni bir `create`
  `platform_admins_email_key` ile reddedilir — yanlış girilen yalnız **ad** idiyse, o adres yeni bir
  hesaba verilemez (yeniden adlandıran bir opadmin eylemi yok) (README O9-1). *(3. tur:)* Bu yol
  kapatılmış hesabın kullanılmamış linkinin ölü olmasına dayanır: `disable` linki silmez (veren
  sütunlar olduğu gibi kalır), onu yalnız `op_complete_enrollment`'ın `AND a.status = 'pending'`
  koşulu (00026) reddeder. Ölçüldü ve pinlendi: hiç kaydolmamış, kapatılmış hesabın linki
  veritabanınca 28000 ile reddedilir, hesap `disabled` ve oturumsuz kalır; kontrol: aynı link
  `disable`'dan önce hesabı kaydeder — `TestDisable_TheUnusedLinkOfADisabledAccountIsRefused`
  (denetçinin X6'sı — koşul `status <> 'active'` — onunla KIRMIZI; denetçinin ölçümünde bu test
  yokken 345 testin hepsi YEŞİL kalmıştı). **Ek savunma adayı** (bu turda yok, opadmin üreticisi
  değişmedi): `disable`'ın link üçlüsünü (`enroll_token_hash`, `enroll_issued_at`,
  `enroll_expires_at`) — CHECK'ler gereği `enroll_used_at` ile birlikte — NULL'laması; link o zaman
  bu koşuldan bağımsız ölü olurdu.
- **LD10** — `scripts/pg-restore-verify.sh` bu tetikleyiciyi denetlemez (yalnız
  `tappa_forbid_mutation`'a bağlı `BEFORE TRUNCATE` korumalarını okur): kapalı ya da eksik bir
  `operator_audit_log_at_is_the_wall_clock` ile geri yüklenmiş bir veritabanı yeşil geçer (önceden de
  böyleydi; backlog orkestratörün).
- **LD11** *(3. tur, güvenlik denetiminin bulgusu)* — **Eski bir opadmin 00033'teki veritabanında
  izsiz çalışır.** Bu commit'ten önceki bir opadmin'in betiği reddedilmez: hesabı değiştirir, audit
  satırı yazmaz (denetçinin ölçümü: HEAD öncesi `disable` hesabı kapattı, satır 0 — geri alınan
  işlemde; bu turda o ikiliyle yeniden üretilmedi). Şemada bir hesap değişikliğinin sahip satırını
  zorunlu kılan bir şey yok. `disable`'ın üretim zamanı koruması yoktur: eski bir `disable` dosyası
  süresiz uygulanabilir. Karşılığı runbook'tadır, şemada değil: *"00033'ten sonra yalnız bu
  commit'in ya da sonrasının opadmin'i kullanılır; eski bir betik iz bırakmaz"* ve README'nin
  Doğrula adımına eklenen sorgu her hesabın son sahip satırını (`kind`, `at`, yaş) okur — ölçen
  `TestRunbook_TheVerifyQueryShowsEachActionsRow`: sorguyu README'den okuyup koşar; `create` ve
  `disable`'dan sonra eylemin türünü ve taze bir yaşı, audit satırı çıkarılmış bir `disable`'dan
  sonra (eski betiğin biçimi, bu üreticiden türetildi) hâlâ `operator_created`'ı gösterir.

**Aşama 2 (2026-10-07, geliştirme veritabanı, PostgreSQL 17.10):** `pg_dump --schema-only`
(`\restrict`/`\unrestrict` ayıklanarak), sha256 ilk 16 hane: v32 `20b156a752860d78` (OP-12 A'nın
v32'si) → `up-by-one` → v33 `d5366ec2e11ea354` → `down` → v32 `20b156a752860d78` → `up-by-one` → v33
`d5366ec2e11ea354`; her adım ayrı komut, sürüm her adımdan önce ve sonra ölçüldü; v32 ile v33'ün
dökümü yalnız iki liste, iki CHECK, fonksiyon ve tetikleyici kadar ayrışır (yetki satırı yok). Down'dan
önce sahip satırı 0 (Down doğrulanmış dala girdi). **Son durum: 33.** `.env`'li `-race`:
`internal/db` 350 PASS, 1 FAIL — `TestOperator00027_DownGivesTheWriteBackAndUpTakesItAgain`, L9'un
`password_ok` 23514'ü (00027'nin Down'ı 33'ün üstünde temiz koştu; yeniden Up'ı 32'deki gibi
düşüyor — sahip satırı yok, 00033'ün payı yok); `cmd/opadmin` 44 PASS; `internal/handler/operator`
108 PASS (`app.css`'li kopyada); `internal/operatorauth` 50 PASS; 0 yarış. Bu kartın testleri hiçbir
satır commit etmez; aşama boyunca sahip türlü satır sayısı 0 kaldı. Mutasyonlar
(`scratchpad/op14d/verify_round1.py`): iki kontrol YEŞİL, **42 adlı mutasyonun 42'si KIRMIZI**,
BUILD-FAILED/APPLY-FAILED/NOT-APPLIED 0, her geri yazma doğrulandı, veritabanı önce = sonra — tablo
OP-14 D kart düzeltmesinde.

**2. tur (2026-10-07, üçüncü gözün ONAY'ı ve altı DÜŞÜK bulgusu — yalnız test ve metin):** migration
SQL'i, Go ürün kodu ve opadmin üreticisi değişmedi; 00033 dosyasında yalnız iki yorum düzeldi
(yorumlar ayıklanınca dosya, geliştirme veritabanına uygulanmış hâliyle aynı; uygulanmış sürüm
yeniden koşturulmadı, veritabanı 33'te). (1) `TestApply_AFailureAnywhereLeavesNoRow`'un reset-mfa
alt testi, yorumunun adlandırıp almadığı sayımı artık alır: hesabın sahip satırları (her sahip türü
ve `operator_mfa_reset`) hatadan önce ve sonra eşit; hata iki yerde enjekte edilir — iki UPDATE
arasında (audit satırından önce) ve iç bloktan sonra (audit satırı yazıldıktan sonra); kontrol:
bozulmamış betik geri alınan bir savepoint'te bir satır fazla sayılır. **Ölçüldü ve itiraz:** audit
INSERT'ünü enjeksiyon noktasından önceye taşıyan iki mutasyonda (M1a: oturum UPDATE'inden önce; M1b:
iç bloğun ilk ifadesi) bu sayım **YEŞİL** kalır — `DO` bloğu tek ifadedir ve `applyInTx` savepoint'i
geri alır: sayım satırın **yerini** değil bloğun **atomikliğini** ölçer; yeri tutan pin
`TestSQL_EachActionWritesOneAuditRowInItsDoBlock`'tur ve ikisinde de KIRMIZI. (2) Sahibin tarih seçme
yolları md. 4, LD2, ADR 0020'nin zaman damgası notu ve migration'ın iki yorumunda: kapatma, düşürme,
fonksiyonu değiştirme (sahiplik ister, süper kullanıcılık değil) ve süper kullanıcının
`session_replication_role = replica` oturumu (belgeden; ölçülmedi). (3) `ByOwner`'ın negatif listesine
`operator_enabled` ve `operator_`, ekran testine bir `operator_enabled` satırı: E17 (`ByOwner` =
`operator_` öneki) artık `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree` ve
`TestAuditScreen_TheOwnersRowsNameTheOwner`'da KIRMIZI; aynı mutasyon 1. turun test sürümlerine karşı
YEŞİL (üçüncü gözün ölçümü yeniden üretildi). (4) LD9 + ölçümü
(`TestAudit_EachActionLeavesExactlyOneRowAndARefusalNone`) + README O9-1. (5) md. 7'nin atfı. (6)
`pg-restore-verify.sh` dokunulmadı — LD10. Mutasyonlar `scratchpad/op14d/verify_round2.py` (yalnız
`op14d-orch` kopyası, yalnız Go kaynak düzenlemesi; mutasyonlu reset-mfa betiği yalnız alt testin geri
alınan işleminde uygulanır, üst düzeyde uygulayan `create` alt testleri mutantla koşmaz): iki kontrol
YEŞİL (bu turun testleri; 1. turun test sürümleri), M1a/M1b ve E17 istenen sonuçta, her geri yazma
doğrulandı, veritabanı önce = sonra (goose 33, sahip satırı 0) — tablo kart düzeltmesinde.

**3. tur (2026-10-07, güvenlik denetiminin ONAY'ı ve üç DÜŞÜK bulgusu — yalnız test ve metin):**
migration SQL'i, Go ürün kodu ve opadmin üreticisi yine değişmedi; 00033'te yalnız bir yorum (md.
4'ün koşulu) düzeldi — yorumlar ayıklanınca dosya uygulanmış hâliyle (`41d52473…`) aynı, yeniden
uygulanmadı, veritabanı 33'te. (1) LD9'un yolunu taşıyan koşul pinlendi:
`TestDisable_TheUnusedLinkOfADisabledAccountIsRefused`; ek savunma adayı LD9'da. (2) Eski bir
opadmin'in izsiz çalışması LD11 olarak sayıldı; README'nin Doğrula adımına hesabın son sahip
satırını okuyan sorgu ve *"00033'ten sonra yalnız bu commit'in ya da sonrasının opadmin'i
kullanılır"* cümlesi eklendi; sorgu README'den okunup koşularak ölçülür
(`TestRunbook_TheVerifyQueryShowsEachActionsRow`). **Kullanıcının adımlarında değişiklik:** yalnız
Doğrula'ya bu ikinci sorgu (uygulama adımları ve beklenen psql çıktısı aynı). (3) PUBLIC
EXECUTE'un gerekçesindeki önkoşul *"tabloda TRIGGER yetkisi"* olarak adlandırıldı (md. 4, migration
yorumu); koşulun pini `TestOperator00026_PrivilegeMatrix`. Mutasyonlar
`scratchpad/op14d/verify_round3.py` (`op14d-orch` kopyası; SQL mutantları testin geri alınan
işleminde, opadmin'in `beginOwnerTx`'ine ve `internal/db`'nin `opTx`'ine geçici kancayla): iki
kontrol YEŞİL; **X6** (`op_complete_enrollment`'ta `status <> 'active'`) yeni testte KIRMIZI —
bütün `cmd/opadmin` paketi X6 altında koşulunca kırmızı olan **yalnız** o; **X7** (`tappa_app`'e
`operator_audit_log`'da TRIGGER) `TestOperator00026_PrivilegeMatrix`'te KIRMIZI; **R1** (README
sorgusu en eski satırı okur) `TestRunbook_TheVerifyQueryShowsEachActionsRow`'da KIRMIZI; her geri
yazma doğrulandı, veritabanı önce = sonra (`op_complete_enrollment`'ın md5'i ve `tappa_app`'in
TRIGGER yetkisi dahil).

**Güvenlik iddiası — üç parça.**

- **Tehdit modeli:** Bu pinler kazara sapmaya karşıdır; bir pini bilerek atlatmak kod
  incelemesinin konusudur. Kapsam: opadmin'in audit satırını, şemanın üçüncü kolunu, tetikleyiciyi
  ve tür kümesinin kopyalarını düşüren, yerinden oynatan ya da genişleten bir düzenleme; ve sahibin,
  tanımlayıcının, `tappa_operator`'ın ve `tappa_app`'in bu tabloya yapabildikleri (ölçülen kollar).
- **PART I — ölçüm ve test adı:**
  - `TestAudit_EachActionLeavesExactlyOneRowAndARefusalNone` (cmd/opadmin) · gerçek betikler sahip
    olarak · her eylem günlüğün sahip satırlarını tam **bir** artırır; satır eylemin türü, hesabın
    id'si, oturum/aktör/tenant/kapsam/sayfa yok, `detail` `{}`, `at` uygulamanın içinde
    (veritabanı saati); ikinci `disable` ikinci satır; altı ret satır bırakmaz (altıncısı, 2. tur:
    devre dışı hesabın adresiyle `create`); yaratılan hesabın sahip `DELETE`'i 23503 (LD9; kontrol:
    iz taşımayan hesap silinir).
  - `TestApply_AFailureAnywhereLeavesNoRow` (cmd/opadmin) · enjekte edilmiş hatalar · ne hesap ne
    satır; pozitif kontrol betiğin işleminde ikisini de görür; reset-mfa'nın audit satırından önce
    ve sonra düşen iki hatası hesabın sahip satırlarını değiştirmez (kontrol: bozulmamış betik bir
    fazla) — bloğun atomikliğini ölçer, satırın yerini değil (2. tur).
  - `TestSQL_EachActionWritesOneAuditRowInItsDoBlock` (cmd/opadmin, DB'siz) · md. 3'ün metni ve yeri;
    kontroller: ikinci satır, blok dışı satır, `at`'li sütun listesi, hesabın yazısından önceki
    satır, başka tür.
  - `TestOperator00033_TheKindsTheShapeAndTheClock` · katalog · iki CHECK'in tam metni ve
    doğrulanmışlığı; tetikleyicinin tanımı ve tablonun üç tetikleyicisi; fonksiyonun sahibi,
    INVOKER'lığı, `proconfig`'i ve tam gövdesi (koşulsuz, donan saat yok); iki fonksiyonun kimliği,
    EXECUTE'u ve 00032'den farkı; sahip türünü adlandıran tanımlayıcı fonksiyonları tam iki;
    taramalar bulgusuz.
  - `TestOperator00033_TheRowsTimeIsTheWallClockWhoeverWritesIt` · sahibin beş yolu ve
    tanımlayıcının iki yolu · satır ifadenin içinde, duvar saatiyle; `at`'in UPDATE'i 23001;
    tetikleyici kapalıyken 2999 durur (sınır 5); L2 kapandı; `DISABLE TRIGGER`'ın kilidi
    `ShareRowExclusiveLock`, başka oturumun INSERT'i ona takılır (55P03).
  - `TestOperator00033_TheOwnerArmIsExactlyAnAccountAndNothingElse` · üç tür × on şekil ·
    `actor_shape` (adıyla) reddeder; küme dışı tür `kind_check`; CHECK'lerin ad sırası;
    `tappa_operator`/`tappa_app` 42501; `op_record_auth_event` 22023; LD3'ün kolu.
  - `TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger` · dört rolün doğrudan çağrısı
    `0A000`; EXECUTE'u geri alınmış fonksiyon tanımlayıcının INSERT'inde yine ateşlenir (md. 4).
  - `TestOpReadAudit_TheOwnersRowsReadAsTheirKindAndAccount` · ürünün taraması · sahip satırı
    türüyle, hesabın id'si ve adıyla, tanınmış; filtre kaydı tanınmış; filtreli okuma yalnız o tür.
  - `TestOperator00033_DownGivesBack00032AndUpTakesItAgain` · dosyanın Down/Up'ı işlem içinde ·
    gövdeler 00032'ninki, koşullar bütün `WHERE` ile, kümeler önceki dosyadan; dallar: boş,
    sahip satırı, sonraki migration'ın türü; zincir 33 → 32 → 31.
  - `TestOperator00033_PreconditionRefusesAWrongCluster` · dokuz yanlış rol şekli ve 00032'nin
    sekiz izi reddedilir (55000, 00033 adıyla); doğru küme geçer.
  - `TestAuditScreen_TheOwnersRowsNameTheOwner` (handler) · sahip satırlarının sözcüğü, hesabı ve
    *"By the platform owner, with opadmin"*; kontrol: `password_ok` *"Before sign-in"*; adlandırılmamış
    iki oturumsuz tür — biri `operator_enabled` — ham metni ve *"Unrecognised"*le, sahibin değil.
  - `TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns` (handler, PostgreSQL) ·
    sözlük = katalogdaki on dört tür.
  - `TestDisable_TheUnusedLinkOfADisabledAccountIsRefused` (cmd/opadmin, 3. tur) · hiç kaydolmamış,
    kapatılmış hesabın kullanılmamış linki · veritabanı 28000 ile reddeder, hesap `disabled`, link
    kullanılmamış, oturum yok; kontrol: aynı link `disable`'dan önce hesabı kaydeder (LD9'un yolu).
  - `TestRunbook_TheVerifyQueryShowsEachActionsRow` (cmd/opadmin, 3. tur) · README'nin Doğrula adımı,
    yazıldığı gibi · `create` ve `disable`'dan sonra eylemin türü, bir dakikadan taze; audit satırı
    çıkarılmış bir `disable`'dan sonra `operator_created` (LD11).
- **PART II — adlı pinler:** `TestSQL_EachActionWritesOneAuditRowInItsDoBlock` (opadmin'in
  on dördüncü pini: tek satır, `DO` bloğunda, yerinde, `(kind, target_admin_id)`, tür ve id,
  `ByOwner` eşitliği); `TestOperator00033_TheKindsTheShapeAndTheClock` (iki CHECK'in tam metni,
  tetikleyicinin tanımı, fonksiyonun gövdesi, iki listenin 00032'den farkı, sahip türünü adlandıran
  tanımlayıcı fonksiyonları); `TestOperator00033_DownGivesBack00032AndUpTakesItAgain` (iki `NOT
  VALID` koşulu bütün `WHERE` ile, Down'ın iki gövdesi, kümeler, `REVOKE`/`GRANT`/`DISABLE
  TRIGGER` yokluğu); `TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree` (kopyalar,
  üç kol ve `ByOwner`'ın kapalı kümesi — `operator_enabled` gibi önekli bir yabancı dahil değil);
  `TestOperatorAuditKinds_TheTypedConstantsAreTheList` (on dört sabit);
  `TestAuditWords_NameEveryKindAndNothingElse` (sözlük = sabitler);
  `TestAuditScreen_NoCredentialFieldReachesThePage` (alan listesi, `ByOwner` dahil);
  `TestOperator00026_PrivilegeMatrix` (tablo düzeyi `TRIGGER` yetkisinin yokluğu — md. 4'ün
  koşulu, 3. tur).
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

## Sonuçlar

- **OP-5:** tablolar (bilet tablosu dahil) + `01-roles.sql` + runbook bu ADR'nin §1 ve
  §5'ini uygular; kabulündeki *"tappa_app dört fiilde yetkisiz"* §1'in ölçü
  sözleşmesiyle ölçülür. **Burada doğanlar:** `op_touch_session`,
  `op_record_auth_event`, `op_open_session`, `op_complete_enrollment`,
  `op_close_session` ve §6'nın ters katalog testleri; reddedilen çağrının audit kararı da
  burada verilir. Bu fonksiyonlar audit yazdığı için §6'nın **ileri yön katalog pini**
  (`proconfig`, PUBLIC ve `tappa_app` `EXECUTE`, ilk argüman, dönüş tipi), **donan saat
  taraması**, **geçici tablo gölgesi testi** (`GRANT` adımıyla) ve **dönüş pini** de
  OP-5'te doğar; **ilk tek aşamalı yazmalar OP-5'te doğan oturum ve kimlik
  fonksiyonlarıdır** (`op_close_session` ile üç oturumsuz fonksiyon) — testler her yeni
  `op_*` için yeniden koşar.
- **OP-7:** `OperatorDB` §3.6 ve §4'ü uygular (okuma = iki transaction).
- **OP-8:** her okuma ekranı önce `op_begin_read`'i commit eder, sonra okur; oturum
  kapısı `op_touch_session`'dır; enrollment handler'ı `op_complete_enrollment`'a bağlanır.
- **OP-14:** operatör audit görüntüleyicisi iki aşamalı `op_read_audit`'tir;
  `tappa_operator`'ın `operator_audit_log` üzerinde `SELECT`'i yoktur. *(A fazı 00031 ile
  uygulandı: okuma, kapalı şekil listesi ve `password_ok` — "OP-14 uygulama notu". B fazı
  ekranı ve wiring'i ekledi — aynı notun "OP-14 B fazı eki". D, 00033 ile opadmin'in üç eylemini
  günlüğe yazdı ve `at`'i duvar saatine bağladı — "OP-14 D uygulama notu".)*
- **OP-12:** bir tenant'ın faturalama ayları iki aşamalı `op_read_tenant_billing`'dir — fatura
  aritmetiğinin üçüncü kopyası, tenant'ın kendi yoluna ay ay bir testle bağlı. *(A fazı 00032 ile
  uygulandı — "OP-12 uygulama notu".)*
- **OP-10** (`op_publish_legal` — `void` bir tek aşamalı yazma; sürüm listesi — ilk
  `op_read_*`) ve **OP-11…OP-18** her yeni `op_*` için §2'nin tamamını ve §6'nın
  katalog/davranış testlerini yeniden kazanır; OP-11'in *"her çağrı tam 1 operatör audit
  satırı"* kabulü *"kabul edilen her okuma `op_begin_read`'de tam 1 satır"* diye okunur.
- **ADR 0002 md.6 yerine getirildi**, metni değişmez; md.7'nin sayımına `op_*`
  **girmez** (ayrı sınıf, bu ADR).
- **Orkestratöre — CLAUDE.md güncellemesi gerekecek** (bu görev CLAUDE.md'ye dokunmaz):
  §4.5 *"uygulama `tappa_app` rolüyle bağlanır"* (OP-7'den sonra ikinci havuz
  `tappa_operator`); §7'nin *"asla loglanmaz"* listesine **okuma bileti, TOTP kodu,
  enrollment token'ı** (§3.5); §3 dizin haritası (ADR 0020 Sonuçlar).
