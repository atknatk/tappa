# ADR 0021 — `op_*`: tenant sınırını aşmanın TEK yolu

- **Durum:** kabul edildi — kullanıcı kararı **D-B** (2026-09-24,
  [m10-platform.md](../plan/m10-platform.md) §6). **Uygulama: yok** — bu ADR
  yazıldığında (HEAD `f6f5a9b`) `tappa_operator`, `tappa_opdefiner`, `platform_*`
  tabloları ve tek bir `op_*` fonksiyonu **yoktur** (katalogda ölçüldü).
  **OP-5 (2026-09-26):** roller, dört tablo ve ilk beş `op_*` migration `00026` ile doğdu —
  bkz. "OP-5 uygulama notu".
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
| **`tappa_opdefiner`** (rol) | **NOLOGIN**, **BYPASSRLS**, NOSUPERUSER, NOCREATEDB, NOCREATEROLE — `tappa_resolver`'ın şekli; **üyesi yok** | `op_*`'ların **sahibi**; fonksiyonların gerektirdiği tablolarda **SÜTUN düzeyi** yetkiler; bilet tablosunda `INSERT` **yalnız bağlama sütunlarında** (hash, oturum, tür, hedef, `audit_id`, `expires_at`) ve `UPDATE` **yalnız `consumed_at`**'te; `platform_admins`'te kimlik bilgisi, durum, TOTP adımı, son giriş ve kilit sayacı sütunlarında `UPDATE` (yazar, digest'i ve zarfı **okumaz**) | aşağıdaki "asla" sütunlarında `SELECT` · **`platform_admins` üzerinde `INSERT`** · bilet tablosunun **`created_at`, `created_xact`, `consumed_at`** sütunlarında `INSERT` (DEFAULT doldurur) ve `consumed_at` dışındaki sütunlarında `UPDATE` · `operator_audit_log`'un **zaman sütununda** `INSERT` (DEFAULT `clock_timestamp()` doldurur) · DEFAULT PRIVILEGE · LOGIN · üye |
| **`tappa_app`** (rol) | değişmez | **hiçbir yeni yetki** | yeni tablolarda ve dizilerde hiçbir fiil · `op_*` üzerinde `EXECUTE` |
| **`op_record_auth_event`** (oturumsuz istisna 1 — fonksiyon) | sahibi `tappa_opdefiner`, `EXECUTE` yalnız `tappa_operator`; **oturum parametresi YOK** | kapalı bir tür kümesinden **yalnız başarısızlık** satırı yazar: `login_failed`, `unknown_email`, `totp_failed`, `locked`, `enrollment_failed`; `RETURNS void` | aktör iddiası · tenant verisi / `tenant_id` · e-posta metni ya da e-postanın herhangi bir hash'i · başarı satırı |
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
adım düşerse rol **hiç** giriş yapamamalı, repodaki bir parolayla yaşamamalı). Giriş,
parolayı bir Secret'tan veren ayrı adımda açılır (§5).

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
- İki DSN'in yer değiştirmesi iki yönde de **gürültülü** başarısızlıktır:
  `tappa_app`'in `op_*` `EXECUTE`'u yok; `tappa_operator`'ın tenant tablolarında
  yetkisi yok.

### 5. Rollerin kurulumu

- **`scripts/db-init/01-roles.sql`:** iki rol (`tappa_operator` NOLOGIN parolasız;
  `tappa_opdefiner` NOLOGIN BYPASSRLS) + `CONNECT`/`USAGE`. **Migration'da değil**,
  çünkü: roller küme düzeyindedir; ve `redline-check.sh` R5b migration'daki bir
  `BYPASSRLS` rolünü FAIL eder — kapsamı bilerek `db/migrations/*.sql`'dir ve
  `01-roles.sql` `tappa_resolver` yüzünden tarama dışıdır (script'in kendi yorumu).
- **`01-roles.sql` yalnız boş `PGDATA`'da koşar** (dosyanın kendi başlığı). Çalışan
  geliştirme veritabanı ve canlı küme için **tek seferlik bir runbook** gerekir; canlı
  kümede bu **kullanıcı işidir** — ajan `tappa-secrets`'a dokunmaz (T44/T45 emsali).
  Geliştirme parolası `tappa_app`'in dev-only script emsaliyle, üretim parolası
  Secret'tan (`deploy/k8s/postgres-init/02-app-password.sh` emsali).
- 🔴 **Sır değerleri hiçbir dosyaya yazılmaz** (Olay A-0). Runbook ve manifestler
  yalnız adlarla konuşur. Kullanıcının `tappa-secrets`'a ekleyeceği adlar (ADR 0020
  Sonuçlar'daki listeyle **aynı**): `TAPPA_OPERATOR_DATABASE_URL`,
  `TAPPA_OPERATOR_TOTP_KEK`, `TAPPA_OPERATOR_TOKEN_HMAC_KEY` ve `tappa_operator`
  rolünün parolası.
  `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest` yeni sır adlarının
  manifestte enjekte edildiğini zorlar.

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
   o fonksiyon üzerinden gürültü basabilir, **o fonksiyon üzerinden** sahte bir başarı
   basamaz (başarı satırları `op_open_session`'ın köken satırlarıdır ve onlar sınır 1'e
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
  kümesi, dördüncü oturumsuz ad) — OP-5/OP-6.
- Enrollment'ta sırrın **gösterildiği** adım ile ilk kodun **doğrulandığı** adım arasında
  düz TOTP sırrının nerede tutulduğu (süreç belleği mi, şifreli ve imzalı kısa ömürlü bir
  ara çerez mi; DB'ye ve log'a asla) — OP-6/OP-8. ⚠️ **"Süreç belleği" seçeneğinin
  riski:** kimliksiz bir `GET /operator/enroll` ile anahtarlanan bir bellek kaydı,
  sınırsız sayıda istekle doldurulabilen bir **bellek ayırma ilkeline** dönüşür; bu
  seçenek seçilirse kayıt sayısı ve ömrü tavanlı olmak zorundadır.
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
  tavan varlık sebebini kaybeder. Sınır 7 olarak sayılı kalır.
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
  dışı; kart madde 33). (2) **
  `log_min_duration_statement` açılırsa** `log_parameter_max_length = -1` ile yavaş bir
  `op_*` çağrısının parametreleri **tam** log'lanır; açılacaksa önce
  `log_parameter_max_length = 0`. Ve bugünkü güvenlik **Go'nun bağlı parametre
  kullanmasına** dayanır: SQL metnine gömülen bir değer `log_statement` / hata STATEMENT'ı
  yoluyla parametre ayarlarından bağımsız log'lanır. **OP-7'ye, adıyla:** operatör
  sorguları yalnız bağlı parametreyle yazılır, SQL metnine değer gömülmez. (Geliştirme
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
  `tappa_operator`'ın `operator_audit_log` üzerinde `SELECT`'i yoktur.
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
