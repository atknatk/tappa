# ADR 0002 — Tenant bağlamı ve RLS stratejisi

- **Durum:** kabul edildi
- **Tarih:** 2026-07-25

> **ADR güncellemesi (2026-07-25, M1-04 uygulaması sırasında).** Bağımsız bir
> tasarım denetçisi madde 7'nin reddettiği "GUC-anahtar saf-RLS" alternatifini
> canlı Postgres'te sınadı. **Kararın kendisi (çevrelenmiş bypass) DEĞİŞMEDİ ve
> doğrulandı** — düzeltilen yalnızca madde 7'nin bir gerekçesidir. İki bulgu
> normatiftir:
>
> 1. **Madde 7'nin "saf RLS bunu ifade edemez / bypass kaçınılmaz" önermesi
>    teknik olarak YANLIŞTI ve geri çekildi.** Bir resolve-GUC'a
>    (`app.resolve_token` gibi) anahtarlanmış RLS dalı çözümlemenin mutlu-yolunu
>    **bypass'sız** ifade eder: `tappa_app` tam RLS'e tabidir, dönüş tek anahtarla
>    sınırlıdır, genel bypass yoktur. Madde 7'nin "bağlamı bilinmeyen satırı
>    okumanın üç yolu, üçü de bypass içerir" listesi bu dördüncü yolu atladığı için
>    eksikti.
> 2. **Ama alternatif yine de REDDEDİLDİ, çünkü çevrelemesi disipline dayanır,
>    YAPIYA değil.** İki tek-nokta hatası canlıda çapraz-tenant ihlal üretti:
>    (a) **okuma:** resolve-GUC `SET LOCAL`'sız kurulursa havuz bağlantısında
>    kalır; toplamsal OR-dalı o bağlantıdaki *her* meşru tenant sorgusuna bir
>    çapraz-tenant satır ekler; `NULLIF` fail-closed bunu **yakalayamaz** ve
>    `WithTenant` `app.tenant_id`'yi ezse de resolve-GUC'a dokunmaz. (b) **yazma:**
>    `FOR ALL USING(...)` kısayolu `WITH CHECK`'i varsayılan olarak `USING`'den
>    **kopyalar** → çapraz-tenant `INSERT` forge edilebilir. İkisi de yalnızca
>    **dikkatle** önlenebilir; §4.5 "tek bir hata sızıntıya dönmemeli" **yapısal**
>    çevreleme ister.
>
> **Sonuç — mekanizma artık somut:** çözümleme, M1-04'te uygulanan `tappa_resolver`
> (NOLOGIN, BYPASSRLS, NOSUPERUSER, default privilege YOK, yalnız `sessions`/`tags`
> tablolarına **sütun-düzeyi** `SELECT`) sahipli bir `SECURITY DEFINER` fonksiyondan
> geçer (`resolve_session_by_token_hash`; anahtar parametreli, ≤1 satır,
> `search_path` sabit `pg_catalog, pg_temp` + tablo `public.`-nitelenmiş, `EXECUTE`
> yalnız `tappa_app`'e, PUBLIC'ten `REVOKE`). Çevreleme artık disipline değil
> **yapıya** dayanır.

> **ADR güncellemesi (2026-07-31, M5-02 A fazı sırasında).** Madde 7'nin sayımı
> **ikiden üçe** çıktı: `GetInviteByCodeHash` (`resolve_invite_by_code_hash`,
> migration [00009](../../db/migrations/00009_create_employee_invites.sql))
> eklendi — bir aktivasyon linki yalnız kod taşır, tenant o aramanın **sonucudur**.
> Üstteki M1-04 bloğunun `tappa_resolver` tarifi de bir tablo genişledi: rol artık
> `sessions`/`tags` **ve `employee_invites`** üzerinde sütun-düzeyi `SELECT`
> tutuyor (başka hiçbir tabloda yetkisi yok — ölçüldü).
>
> **Karar değişmedi**; değişen yalnız sayımdır ve sayı hiçbir zaman garanti
> değildi: garanti maddedeki **beş normatif kısıt**tır. Üçüncü lookup beşini de
> canlı katalogda karşıladı (ölçüldü, varsayılmadı): girdi yalnız anahtar
> (`code_hash`); dönüş ≤1 satır — bunu fonksiyon gövdesi değil `code_hash`
> üzerindeki **global UNIQUE indeks** garanti eder; sabit sütun listesi, `SELECT *`
> yüzeyi yok (`RETURNS TABLE(id, tenant_id, employee_id, expires_at, used_at)` —
> `code_hash` ve `created_at` dönmez); `tappa_app`'in tabloda çapraz-tenant
> `SELECT`'i yok, yalnız fonksiyonda `EXECUTE` var (`has_function_privilege`:
> PUBLIC `f`, `tappa_app` `t`); politikada naif "bağlam NULL iken göster" dalı yok
> — standart `NULLIF` politikası. Definer yine `tappa_resolver` (`rolsuper=f`,
> `rolbypassrls=t`), `search_path=pg_catalog, pg_temp` sabit, tabloya
> **sütun-düzeyi** SELECT (`created_at` dahil DEĞİL).
>
> Canlı ölçüm ayrıca şunu gösterdi (bağlamsız `tappa_app`, tek transaction):
> tabloya doğrudan `SELECT` **0 satır** (FORCE RLS, fail-closed), aynı bağlamda
> fonksiyon **1 satır**, bağlam kurulunca doğrudan okuma yine **1 satır** (pozitif
> kontrol) — yani çevreleme çalışıyor ve sonda boş tablo üzerinde koşmuş değil.
> Kalıcı testi: `internal/db/invites_test.go`
> (`TestResolveInvite_SecurityDefinerProperties`).
>
> **Kural, sayı değil sınırdır:** dördüncü bir çözümleme sorgusu eklenecekse aynı
> beş kısıtı karşıladığını **ölçerek** göstermek zorundadır; "zaten üç tane var"
> gerekçe değildir.

> **ADR güncellemesi (2026-08-02, M6-01 A fazı sırasında) — bu blok bir kısıtı
> DEĞİŞTİRİYOR, yalnız sayımı değil. Dikkatle okuyun.** Madde 7'nin sayımı
> **üçten beşe** çıktı: `GetAdminByEmail` (`resolve_admin_by_email`) ve
> `GetAdminSessionByTokenHash` (`resolve_admin_session_by_token_hash`), migration
> [00011](../../db/migrations/00011_add_admin_resolution.sql). Gerekçe: panele tek
> bir giriş adresinden gelinir, istek yalnız e-posta+parola (ya da yalnız çerez)
> taşır — tenant yine o aramanın **sonucudur**. `tappa_resolver` iki tablo daha
> gördü: `admin_users` ve `admin_sessions` üzerinde **sütun-düzeyi** `SELECT`
> (başka hiçbir tabloda yetkisi yok — ölçüldü).
>
> **KULLANICI KARARI (2026-08-02): global çözümleme + tenant seçici.** Tek giriş,
> tek e-posta; e-posta global çözülür, parola doğrulanır, e-posta birden fazla
> işletmede kayıtlıysa **kimlik doğrulandıktan SONRA** "hangi işletme?" ekranı
> gelir. Gerekçe: iki design partner tenant'ı (KF + KM) **aynı kişiye** ait.
> Reddedilen alternatif — tenant'ı imzalı çerezde/alt alan adında taşımak: tenant
> çözümlemenin **çıktısıdır**, girdisi değil; girdi alan bir middleware çağıranın
> kendi tenant'ını adlandırmasına izin verir (M5-03). İmza bunu değiştirmez: imza
> değeri **bizim** ürettiğimizi söyler, **hangi oturuma** ait olduğunu değil.
>
> **Değişen kısıt — (ii) "dönüş en çok BİR satır" e-posta lookup'ı için GEÇERSİZ.**
> Diğer dördünün anahtarı **global** tekildir (`tags.uid` PK,
> `sessions.token_hash`, `employee_invites.code_hash`, `admin_sessions.token_hash`)
> — hepsi ≤1 satırı korur. Admin e-postası **yalnız tenant içinde** tekildir
> (`admin_users_tenant_email_key`), çünkü aynı kişi iki işletmenin sahibi olabilir;
> bu ürün kararıdır, bir eksiklik değil. (ii) yerine geçen **ölçülmüş sınır**:
>
> > tenant başına **en çok bir** satır — bunu fonksiyon gövdesi değil kısmi
> > **UNIQUE (tenant_id, email)** indeksi garanti eder → toplamda en çok
> > `count(tenants)` satır, ve sonuçta **aynı tenant_id iki kez görünemez**.
>
> Sınır 1/2/3 tenant'la ve bilinmeyen e-postayla (0 satır) canlı ölçüldü:
> `internal/db/admins_test.go` → `TestGetAdminByEmail_RowBound`. `LIMIT`
> **konmadı**: bir LIMIT, kullanıcının gerçekten üye olduğu bir işletmeyi sessizce
> düşürürdü. Kalan dört kısıt ikisinde de aynen geçerli ve canlı katalogda
> ölçüldü (`TestResolveAdmin_SecurityDefinerProperties`): girdi yalnız anahtar;
> sabit sütun listesi (`SELECT *` yüzeyi yok); `tappa_app`'in tabloda çapraz-tenant
> `SELECT`'i yok, yalnız fonksiyonda `EXECUTE` (PUBLIC `f`, `tappa_app` `t`);
> politikalarda naif "bağlam NULL iken göster" dalı yok. Definer yine
> `tappa_resolver` (`rolsuper=f`, `rolbypassrls=t`), `search_path=pg_catalog,
> pg_temp` sabit.
>
> **Yeni bir sınır adlandırıldı: e-posta numaralandırma.** `tappa_app`
> `resolve_admin_by_email`'i çağırabildiği için uygulama, parola olmadan "bu
> e-posta kayıtlı mı" sorusunu cevaplayabilir. Bu, global çözümlemenin **doğasında**
> vardır (kimlik doğrulama önce kimliği bulmak zorundadır) ve **veritabanında
> kapatılamaz**. Kapatma yükümlülüğü M6-01 **B fazı** handler'ınındır: üç sonuç
> (bilinmeyen e-posta / yanlış parola / devre dışı admin) için **aynı** yanıt, 0
> satır dönünce **kukla bcrypt** karşılaştırması (bcrypt kasıtlı olarak yavaştır;
> atlamak ölçülebilir bir zamanlama sızıntısıdır), oran sınırı ve `audit_log`.
> Sınır hem 00011'in "PHASE B OBLIGATION" listesine, hem `db/queries/resolve.sql`'e,
> hem `internal/db/resolve.go`'daki çağrılabilir fonksiyonun doküman yorumuna, hem
> de M6-01 kartına (`docs/plan/m6-dashboard.md`) yazıldı — B fazının okumadan
> geçemeyeceği yerler. **Aşağıdaki bcrypt amplifikasyonu sınırı da aynı dört yere
> yazıldı.**
>
> **Ölçülmüş bir tuzak, kalıcı kayıt:** `citext` sütununda **`=` operatörü
> `public` şemasındadır**; `search_path = pg_catalog, pg_temp` sabitlendiğinde
> operatör görünmez olur ve Postgres **hata vermeden** citext→text örtük cast'iyle
> `text = text`'e düşer → arama sessizce **büyük/küçük harfe duyarlı** olur
> (ölçüldü: kayıtlı `Owner@Example.Test` için tam yazım 1 satır, küçük harf 0,
> büyük harf 0). Düzeltme: gövdede **`OPERATOR(public.=)`**. Bu yalnız kullanılabilirlik
> değil doğruluk meselesidir: farklı bir eşitlik kullanan bir gövde, yukarıdaki
> satır sınırını garanti eden UNIQUE indeksin eşitliğinden **sapardı**. İki test,
> iki ayrı iş — **atıf önemlidir, karıştırılırsa greplayan okur ya ADR'nin ya
> testin yalan söylediğini sanır:** `TestGetAdminByEmail_IsCaseInsensitive`
> **sevk edilen** çözümleyiciyi üç yazımla (tam / küçük / büyük harf) çağırıp
> üçünde de 1 satır bulur; **kalıcı negatif kontrol ayrı bir testtir** —
> `TestGetAdminByEmail_CaseInsensitivityNegativeControl`, aynı sabitlenmiş
> `search_path` altında **niteliksiz `=`** ile bozuk bir ikiz gövde kurar, küçük
> harfte **0 satır** bulduğunu gösterir ve gövdeyi düşürür. Onsuz ilk test yalnız
> "citext bir yerde var" derdi, `OPERATOR(public.=)`'in yük taşıdığını değil.
>
> **Ölçülmüş bir sınır daha, ve numaralandırmadan AYRI: bcrypt amplifikasyonu.**
> Çözümleyici N satır döndürür, B fazı her satırı bcrypt'ler; pahalı yarı
> **çağıranın** tarafındadır. Ölçüldü: 500 tenant'a ekilmiş tek bir adres **500
> satır**ı sıcakta ~0,9–1,2 ms'de döner (**darboğaz veritabanı değil**), buna
> karşılık cost-10 bir bcrypt ~60–100 ms → tek **kimliksiz** `POST /login`
> ~30–50 sn CPU, yani ~**500×** amplifikasyon (bir **DoS** sınırı, zamanlama
> sınırı değil). Bugün sömürülemez (repoda tenant yaratan sorgu yok), ama
> **M7-02'nin herkese açık kayıt sihirbazıyla satır sayısını saldırgan belirler**
> — RLS bir tenant'ın kendi `admin_users`'ına istediği e-postayı yazmasını
> engellemez. Seçenekler (aday sayısına üst sınır · ilk eşleşmede durma · M7-02'de
> e-posta doğrulaması) **B fazının kendi ölçümüyle** kararlaştırılacak; bu ADR
> hiçbirini seçmez.
>
> **Kural, sayı değil sınırdır** (yukarıdaki blok): altıncı bir çözümleme sorgusu
> aynı kısıtları **ölçerek** göstermek zorundadır — ve bir kısıt bu blokta olduğu
> gibi geçerliliğini yitiriyorsa, yerine geçen sınır **yazılmak ve ölçülmek**
> zorundadır.

> **ADR güncellemesi (2026-08-14, M7-04 A fazı sırasında).** Madde 7'nin sayımı
> **beşten altıya** çıktı: `GetPasswordResetByTokenHash`
> (`resolve_password_reset_by_token_hash`), migration
> [00019](../../db/migrations/00019_harden_password_resets.sql). Gerekçe diğer
> beşiyle aynı sınıftadır: bir **parola sıfırlama linki yalnız token taşır**, tenant
> o aramanın **sonucudur**. Ölçüldü — bağlamsız `tappa_app` olarak
> `password_resets`'e doğrudan `SELECT` **0 satır** (FORCE RLS, fail-closed), aynı
> koşulda fonksiyon **1 satır**; yani çözümleyici olmadan akış var olamaz.
> `tappa_resolver` bir tablo daha gördü: `password_resets` üzerinde **sütun-düzeyi**
> `SELECT` (`created_at` listede **değil**).
>
> **Karar değişmedi, kısıt de değişmedi** — bu blok yalnız sayımı ve bir tablo
> adını günceller. Üstteki M6-01 bloğunun (ii)'yi geçersiz kılan istisnası **buraya
> yayılmaz**: bu aramanın anahtarı `token_hash` ve o **global UNIQUE** (00006), yani
> ≤1 satır yeniden **yapısaldır**. Beşi de canlı katalogda ölçüldü, varsayılmadı:
> `internal/db/passwordresets_test.go` →
> `TestResolvePasswordReset_SecurityDefinerProperties` (girdi yalnız anahtar
> `p_token_hash text`; ≤1 satır UNIQUE indeksten; sabit sütun listesi —
> `token_hash` **girdidir ve dönmez**, `created_at` da dönmez; PUBLIC `EXECUTE` `f`,
> `tappa_app` `t`; politikada naif "bağlam NULL iken göster" dalı yok; definer
> `tappa_resolver`, `rolsuper=f`, `rolbypassrls=t`, `search_path` sabit).
> **Test mutasyonla doğrulandı:** definer `tappa_owner`'a çevrilince ve PUBLIC
> `EXECUTE` geri verilince ayrı ayrı **kırmızı**.
>
> **Reddedilen alternatif, ve M5-03/M6-01 gerekçesinin aynısı:** tenant'ı reset
> LİNKİNE koymak. Token yine eşleşmek zorunda olsa da, bağlamı **çağıran
> adlandırmış** olurdu — o transaction'daki her ifade saldırganın seçtiği tenant'ta
> koşar. **İkincisi (daha ince) reddedilen alternatif:** kullanıcıya reset
> sayfasında e-postasını tekrar yazdırıp `resolve_admin_by_email`'i yeniden
> kullanmak. Yeni bypass yüzeyi eklemez ve **birinci gerekçe onu tek başına eler**
> (tenant girdi değil sonuçtur; link e-posta değil token taşır).
>
> ⚠️ **Bu blok ikinci bir gerekçe daha yazıyordu ve o bir VARSAYIMDI — denetim
> ölçtü, geri çekiliyor.** *"Sıfırlama o zaman `adminauth.MaxCandidates` penceresini
> miras alır"* doğru değil: `resolve_admin_by_email`'in gövdesinde `LIMIT` **yok**
> (ölçüldü; 00011 bunu zaten açıkça yazıyor), `MaxCandidates = 8` bir **Go
> sabitidir** (`manager.go:118`) ve yalnız `Authenticate`'in aday döngüsüyle bcrypt
> dolgusunda kullanılır (`manager.go:442/443/579` — başka çağrı yeri yok). Bir
> sıfırlama isteği aday başına bcrypt **ödemediği** için o pencereyi miras almak
> **zorunda değildir**. Ölçüye indirilmiş doğru ifade: e-postadan aday çözen bir
> sıfırlama, aday daraltma kararını **kendi tehdit modeline göre sıfırdan vermek**
> zorunda kalır ve yanlış verirse 00017'nin belgelenmiş artık-kilidini kendi
> yüzeyinde yeniden üretebilir.
>
> **Yeni bir numaralandırma kanalı AÇILMADI** ve bu ölçülebilir bir ifadedir: bu
> aramanın tek girdisi 256 bitlik rastgele bir değerdir ve çıktısı **hiçbir kişiyi
> adlandırmaz** (ad, e-posta, rol, durum dönmez). Karşılaştırma: `GetAdminByEmail`
> doğası gereği bir kehanettir ve B fazı yükümlülükleriyle kapatılır.

## Bağlam

Tappa çok kiracılı (multi-tenant) bir SaaS: iki design partner (Kebab Factory —
9 lokasyon; Kebab Manufacturing — 5 departman) ve ileride daha fazlası **aynı**
veritabanını paylaşır. [CLAUDE.md](../../CLAUDE.md) §4.5 kırmızı çizgisi tenant
izolasyonunu **her katmanda** zorunlu kılar: her tabloda `tenant_id`, her tabloda
RLS politikası, uygulamanın RLS'e tabi bir rolle bağlanması ve sorgularda ayrıca
açık `tenant_id` filtresi ("kuşak + kemer").

Bundan sonraki her migration ([M1-02](../plan/m1-veri-katmani.md)…M1-06, M1-11) ve
her sorgu ([M1-08](../plan/m1-veri-katmani.md)) bu karara uyacak. Sıfır hafızalı
bir ajan yazılı karar yoksa kendi yorumunu üretir ve izolasyon **sessizce**
delinir. Bu ADR o kararı normatif olarak sabitler.

Kararın üç girdisi M0-03 uygulamasında **canlı Postgres üzerinde ölçüldü**,
varsayılmadı (ayrıntı: Gerekçe → normatif ölçümler):

1. `app.tenant_id` GUC'una bir kez **yazıldıktan** sonra bağlantıda asla `NULL`'a
   dönmez, `''` kalır (`ROLLBACK`/`RESET`/`DISCARD ALL` üçü de) →
   [Q27](../plan/open-questions.md).
2. `tappa_owner`, docker-compose'un `POSTGRES_USER`'ı olarak initdb'nin
   **bootstrap superuser'ıdır** (`rolsuper=t`); superuser RLS'i koşulsuz atlar.
3. `FORCE ROW LEVEL SECURITY` **tablo sahibini** politikaya tabi kılar ama
   **superuser'ı** bağlamaz.

Ayrı bir yapısal kısıt da kararı şekillendirir: bir tap geldiğinde elde yalnız
tag UID (URL'den) ve oturum çerezi vardır; **ikisi de tenant taşımaz.**
`app.tenant_id` tam da bu iki aramanın *sonucudur*. Yani "her erişim tenant
bağlamında koşar" kuralı, bağlamı **kuran** aramalar için sağlanamaz — bu ADR'nin
çözmesi gereken çelişki budur (Karar madde 7). *(Bu paragraf **tap akışını**
anlatır ve o akışta arama sayısı ikidir; aynı şekil sonradan aktivasyon linkine
(M5-02) ve panel girişine (M6-01) de çıktı — **güncel ve tam liste madde 7'dedir,
bu paragraf sayı kaynağı değildir.**)*

## Karar

**1. Rol ayrımı.** Uygulama `tappa_app` rolüyle bağlanır: `NOSUPERUSER`,
`NOBYPASSRLS`, tablo sahibi **değil**. Migration'lar ayrı `tappa_owner` rolüyle
koşar (`DATABASE_MIGRATE_URL`). `internal/config` iki ayrı bağlantı dizesi ister
ve onları eşitlemeyi reddeder; ayrım config tarafından **zaten** zorlanır.
`tappa_app`'e `BYPASSRLS` **asla** verilmez.

**2. Bağlam transaction başına kurulur.** Tenant bağlamı her transaction'ın
başında `set_config('app.tenant_id', $1, true)` ile kurulur. Üçüncü argüman
`true` = transaction-local; bağlam transaction dışına **sızmaz**. `SET LOCAL`
parametre bağlama (`$1`) kabul etmediği için `set_config(...)` kullanılır; tenant
değeri **asla** string birleştirmeyle SQL'e gömülmez. Havuzdan alınan bağlantıda
`LOCAL`'siz `SET` **yasaktır**: bağlantı havuza kirli döner ve bir sonraki tenant
onun bağlamıyla sorgu koşar.

**3. Politika ifadesi — normatif ve birebir.** Her tenant politikası şu ifadeyi
yazar:

```
NULLIF(current_setting('app.tenant_id', true), '')::uuid
```

- `tenants` tablosunda: `id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`
- Diğer tüm tablolarda: `tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid`

`current_setting`'in ikinci argümanı `true`: ayar yoksa hata değil `NULL` döner.
`NULLIF(..., '')` ise GUC'un `''`'e düşmüş hâlini de `NULL`'a çevirir. Sonuç:
bağlam yokken ifade **her zaman** `NULL` üretir, hiçbir satır eşleşmez →
**fail-closed** (ne satır sızar ne sessiz onay — §4.6 korunur). Bu davranış
[M1-09](../plan/m1-veri-katmani.md) vaka 3'te test edilir. Çıplak biçim
(`current_setting('app.tenant_id', true)::uuid`) **terk edildi** (gerekçe:
Gerekçe → Q27 tablosu ve Değerlendirilen alternatifler).

**4. Kuşak + kemer: sorgularda ayrıca açık filtre.** Üretim sorgularının hepsi
RLS'e **ek olarak** açık `tenant_id = @tenant_id` filtresi taşır. RLS'in
kurulmadığı bir kod yolunda (unutulan `FORCE`, yanlış rol, ileride eklenen bir
tablo) tek savunma budur. **Tek istisna** madde 7'deki tenant çözümleme yoludur.

**5. `tenants` tablosunun kendisi de korunur.** `tenants`, `id` üzerinden aynı
politikayı alır (madde 3, `tenant_id` yerine `id`). Politikasız bir `tenants`
tablosu müşteri isimlerini ve VAT numaralarını sızdırır.

**6. Süper-admin / çapraz-tenant erişim MVP'de yoktur.** Böyle bir ihtiyaç
doğarsa ayrı bir rol ve ayrı bir ADR ile gelir. `tappa_app` bu amaçla asla
ayrıcalıklandırılmaz; ona `BYPASSRLS` verilmez.

**7. Tenant çözümleme istisnası — bu ADR'nin en kritik maddesi.** Yalnızca beş
sorgu — `GetTagByUID`, `GetEmployeeBySessionHash`, (M5-02'de eklenen)
`GetInviteByCodeHash` ve (M6-01 A fazında eklenen) `GetAdminByEmail` +
`GetAdminSessionByTokenHash` — tenant bağlamı kurulmadan koşar, çünkü bağlam
onların **sonucudur**. *(Bu sayı 2026-07-31'e kadar "iki", 2026-08-02'ye kadar
"üç"tü; sayı garanti değildir, garanti aşağıdaki beş kısıttır — baştaki M5-02 ve
M6-01 güncelleme blokları. **`GetAdminByEmail` için (ii) "≤1 satır" kısıtı
GEÇERSİZDİR** ve yerini ölçülmüş bir tenant-başına-bir sınırına bırakır; ayrıntı
M6-01 bloğunda.)* Bu istisna dar, adlandırılmış ve testlidir:

- **Ayrı ve görünür.** **Beşi de** `db/queries/resolve.sql` dosyasında durur
  (üretim sorgularının geri kalanından ayrı), böylece §4.5'in "her sorguda açık
  `tenant_id` filtresi" kuralının **tek istisnası** koda bakıldığında görünür
  kalır. **(Uygulama notu — M1-08, 2026-07-25:** sqlc v1.28 `RETURNS TABLE(...)`
  döndüren fonksiyon çağrılarını **tipleyemiyor** (ölçüldü) → bu iki lookup
  `internal/db/resolve.go`'da **elle, tipli** yazıldı; `resolve.sql` `-- name:`'siz
  **kanonik-SQL belgesi** olarak kaldı (sqlc atlar). Güvenlik özellikleri değişmedi:
  ikisi de yalnız `resolve_tag_by_uid`/`resolve_session_by_token_hash` SECURITY
  DEFINER fonksiyonlarını çağırır, çıplak tablo erişimi yok, bağlamsız ham havuzda.
  İki denetçi doğruladı. Elle-senkron drift riski M1-09 test kapsamına devredildi.**)
  **(M5-02, 2026-07-31: aynı ölçüm 00009'un `resolve_invite_by_code_hash`'i için
  tekrarlandı — sqlc v1.28 birebir aynı davranıyor: açık sütun listesi `column "id"
  does not exist`, `SELECT *` ise `interface{}` — bu yüzden üçüncü lookup da elle
  yazıldı.)** **(M6-01 A fazı, 2026-08-02: ölçüm 00011'in
  `resolve_admin_by_email`'i için üçüncü kez tekrarlandı, sonuç yine aynı — bu
  yüzden dördüncü ve beşinci lookup da `internal/db/resolve.go`'da elle yazıldı.)**
- **`tappa_app` rolüyle, `tenant_id` filtresi olmadan.** Aramaların dördü küresel
  olarak tekil bir anahtara dayanır (`tags.uid` birincil anahtar;
  `sessions.token_hash`, `employee_invites.code_hash`, `admin_sessions.token_hash`
  UNIQUE), dolayısıyla tenant bilinmeden de en çok **bir** satır döner. **Beşincisi
  (`GetAdminByEmail`) dönmez** — admin e-postası yalnız tenant içinde tekildir;
  sınırı kısmi `UNIQUE (tenant_id, email)` indeksinden gelir: tenant başına en çok
  bir satır, aynı tenant iki kez görünemez (M6-01 güncelleme bloğu, ölçüldü).
- **Neden saf RLS değil — gerekçe "ifade edilemezlik" DEĞİL, YAPISAL çevrelemedir.**
  *(Bu maddenin önceki hâli — "saf RLS bunu tek başına ifade edemez, bypass
  kaçınılmazdır" — canlı Postgres sondasıyla çürütüldü ve düzeltildi; bkz. baştaki
  ADR güncellemesi ve Değerlendirilen alternatifler.)* Bir resolve-GUC'a
  anahtarlanmış RLS dalı çözümlemenin mutlu-yolunu **bypass'sız** ifade edebilir:
  `tappa_app` tam RLS'e tabi kalır, dönüş tek anahtarla sınırlıdır, genel bypass
  yoktur. Dolayısıyla çözümleme bir bypass'ı **kaçınılmaz kılmaz.** Alternatif yine
  de reddedildi çünkü çevrelemesi **disipline** dayanır, yapıya değil: (a) `SET
  LOCAL` unutulan tek bir kod yolunda resolve-GUC havuz bağlantısında **kalır** ve
  toplamsal OR-dalı o bağlantıdaki *her* meşru sorguya çapraz-tenant satır ekler
  (`NULLIF` fail-closed bunu yakalayamaz; `WithTenant` `app.tenant_id`'yi ezse de
  resolve-GUC'a dokunmaz); (b) `FOR ALL USING(...)` kısayolu `WITH CHECK`'i
  sessizce `USING`'den kopyalayıp çapraz-tenant `INSERT` forge'una izin verir. §4.5
  "tek bir hatanın sızıntıya dönmemesi"ni ister; bu yüzden çevreleme **yapıya**
  dayanmalı. Seçilen yapıda: bağlamı bilinmeyen bir satırı §6 FORCE altında okumak
  zaten bir `BYPASSRLS` gerektirir (RLS `USING`/`WITH CHECK` satır bazlı bir
  **boolean**'dır ve sorgunun `WHERE`'ini göremez; "yalnız anahtar aramasına izin
  ver, `SELECT *`'a verme" sınırı RLS'in kendisiyle değil aşağıdaki dar arayüzle
  konur). Karar bu bypass'ı **yok etmek değil, çevrelemektir** — en-az-ayrıcalıklı
  bir role ve tek bir `SECURITY DEFINER` fonksiyona; genel bir bypass (superuser /
  geniş `BYPASSRLS` / naif "bağlam `NULL` iken tüm satırlar" dalı) açmadan.
- **Güvenlik RLS'ten değil ARAYÜZDEN gelir.** Bağlam yokken çapraz-tenant okuma
  yalnızca dar, anahtar odaklı bir arayüzün ardında yapılır ve şu özellikleri
  **normatif** olarak taşır: (i) girdi yalnız **anahtar**dır (`uid` /
  `token_hash` / `code_hash` / **`email`**); (ii) dönüş **yapısal olarak
  sınırlıdır ve sınırı gövde değil bir UNIQUE indeks verir** — anahtarı **global**
  tekil olan dörtte bu en çok **bir** satırdır (`tags.uid`,
  `sessions.token_hash`, `employee_invites.code_hash`,
  `admin_sessions.token_hash`); **`GetAdminByEmail`'de değildir**, çünkü admin
  e-postası yalnız tenant içinde tekildir — orada sınır kısmi
  `UNIQUE (tenant_id, email)` indeksinden gelir: **tenant başına en çok bir**
  satır, toplamda en çok `count(tenants)` satır, aynı `tenant_id` iki kez
  görünemez (ayrıntı ve ölçüm: M6-01 güncelleme bloğu); (iii)
  **`SELECT *` yüzeyi yoktur** — sabit, yalnız gerekli sütunları içeren liste;
  (iv) çağıran `tappa_app`'in bu tablolarda doğrudan çapraz-tenant `SELECT`
  hakkı **yoktur**, yalnız bu dar arayüz üzerinde `EXECUTE` hakkı vardır; (v)
  "bağlam `NULL` iken satırı göster" biçiminde naif bir RLS izin dalı
  **yasaktır** — o, `SELECT * FROM tags`'i tüm tenant'lara açan yukarıdaki (c)
  şıkkıdır. Dönüşün sınırlı kalması, gövdeye bakılırsa yalnız bir *gövde
  özelliği*dir; yapısal garanti bu beş kısıttan (ve (ii)'de adı geçen UNIQUE
  indekslerden) gelir, gövdenin iyi niyetinden değil.
- **En-az-ayrıcalık: definer superuser OLAMAZ.** Arayüz bir `SECURITY DEFINER`
  fonksiyonla kurulacaksa sahibi **superuser `tappa_owner` DEĞİLDİR.** Gerekçe:
  `SECURITY DEFINER` gövdesi **sahibinin** yetkisiyle koşar; sahip superuser ise
  gövde RLS'i **tümüyle** atlar ve arayüz kusurlu olduğunda patlama yarıçapı
  **tüm veritabanıdır** — bu tam da yasaklanan genel bypass'tır (bkz.
  Değerlendirilen alternatifler). Doğrusu — ve §6'yla birlikte okunmalı: **§6 her
  tabloya `FORCE ROW LEVEL SECURITY` zorunlu kıldığı için** bağlam yokken salt-
  `SELECT` yetkisi **yetersizdir** — RLS'e tabi bir rol 0 satır görür; okumak bir
  bypass ister. §6 FORCE altında **tek uyumlu bypass `BYPASSRLS`**'tir (FORCE'suz-
  sahip yolu §6 altında kapalı; superuser yasak). Yani definer'ın çevrelenmiş
  bypass'ı, `BYPASSRLS` olan ama yalnız bu tablolara (gerekli sütunlarda)
  `SELECT` GRANT'i verilmiş, adanmış ve **en-az-ayrıcalıklı** bir roldür; patlama
  yarıçapı o role verilen GRANT'larla bu tablolara sınırlanır, tüm DB değil.
  Böylece M1-04/M1-05 uygulayıcısı bypass'sız salt-`SELECT` bir rol kurup 0
  satırla şaşırmaz. Kesin rol adı ve DDL [M1-04](../plan/m1-veri-katmani.md)
  (sessions) ve [M1-05](../plan/m1-veri-katmani.md) (tags) migration'larında
  sabitlenir, sınırı [M1-09](../plan/m1-veri-katmani.md)'da test edilir; bu ADR
  yalnız **sınırı ve reddedilenleri** normatif koyar.
- **Çözümlemeden sonra bağlam kurulur.** Bu beş aramadan hangisi koştuysa o
  tamamlanınca `app.tenant_id` ayarlanır ve **geri kalan her şey** `WithTenant`
  içinde koşar (madde 2). `GetAdminByEmail` birden çok aday döndürdüğünde bağlam
  **her aday için ayrı ayrı** kurulur (tenant seçici); adaylar arasında paylaşılan
  tek bir bağlam yoktur.
- **`sys:tenant-mismatch` guardrail'i.** `token_hash` küresel UNIQUE olduğu için
  arama farklı tenant'ların Tag'i ve Session'ıyla başarılı olabilir
  (`Employee{KM}` + `Tag{KF}`). Etiketin tenant'ı ile oturumun tenant'ı
  ayrıştığında `sys:tenant-mismatch` guardrail'i
  ([M3-05](../plan/m3-policy-motoru.md), sıra 1) akışı keser.

Bu istisna yazılmazsa uygulama ya ayrıcalıklı bir rol ekler (madde 1/6 fiilen
delinir) ya da tap akışı hiç çalışmaz; ikinci hâlde geliştirici tenant
izolasyonundaki tek deliği **tam da kimlik doğrulama yolunda** açmaya itilir.

## Gerekçe

### Neden RLS'e ek olarak uygulama filtresi (kuşak + kemer)

RLS tek başına güçlüdür ama tek noktalıdır: bir tabloda `FORCE` unutulursa,
`tappa_app` yanlışlıkla ayrıcalıklandırılırsa ya da yeni bir tablo politikasız
doğarsa izolasyon **sessizce** açılır. Açık `tenant_id` filtresi bu durumda
ikinci bir bariyerdir. Tersi de doğrudur: bir sorguda `WHERE tenant_id` unutulsa
RLS yakalar. İkisi birlikte, tek bir hatanın çapraz-tenant sızıntıya dönmesini
engeller. Bu yüzden filtre RLS'in **yerine** değil, **yanına** konur.

### Q27 — neden `NULLIF`, neden çıplak cast değil (normatif ölçüm)

`tappa_app` ile canlı sonda:

| bağlantının durumu | `current_setting('app.tenant_id', true)` | çıplak `::uuid` | `NULLIF(...)::uuid` |
|---|---|---|---|
| GUC'a **hiç yazılmamış** | `NULL` | `NULL` → 0 satır | `NULL` → 0 satır |
| bir kez **yazılmış**, tx bitmiş | `''` | **`ERROR: invalid input syntax for type uuid: ""`** | `NULL` → 0 satır |

Tetikleyici bağlantının kaçıncı kullanımı değil, GUC'a **ilk yazma**dır: taze bir
bağlantıda arka arkaya üç bağlamsız sorgu üçünde de `NULL` / 0 satır / hatasız
geçti. `''`'e düştükten sonra `ROLLBACK`, `RESET app.tenant_id` ve `DISCARD ALL`
de `NULL`'a döndürmez (üçü ayrı ayrı ölçüldü); tek yol **yeni bağlantı**dır.
`pgxpool` altında bu, ilk `SET LOCAL`'dan sonra o bağlantı için **kalıcı**
durumdur.

İki biçim de **fail-closed**; kırılan şey güvenlik değil **determinizm**: aynı
hata (unutulmuş bağlam), bağlantı geçmişine göre iki farklı davranış üretir —
taze bağlantıya karşı yazılmış bir test geçer, üretim patlar. `NULLIF` her iki
durumda da `NULL` ürettiği için seçildi; uçtan uca doğrulandı (aynı bağlantıda
tenant A → 1 satır, commit sonrası bağlamsız → 0 satır, tenant B → 1 satır, hata
yok).

**Bedeli açıkça:** `NULLIF` unutulan bağlamı **sessizce** 0 satıra çevirir;
çıplak biçimin gürültülü hatası bir bug'ı daha çabuk yakalardı. Bu bedel giriş
noktasında telafi edilir: [M1-07](../plan/m1-veri-katmani.md) `WithTenant`
sarmalayıcısı `set_config('app.tenant_id', $1, true)`'i **kendisi** kurar ve
bağlam kurulmadan sorgu çalıştırmayı **API olarak imkânsız** kılar. Yani koruma
politikadan değil giriş noktasından gelir.

### M0-03 — `tappa_owner` superuser'dır; `FORCE` onu bağlamaz (normatif ölçüm)

`FORCE ROW LEVEL SECURITY`'nin kendisi **çalışır**: geçici yaratılan
**NOSUPERUSER** bir tablo sahibi (`rolsuper=f`, `rolbypassrls=f`), `ENABLE`+`FORCE`
RLS'li kendi tablosunu bağlam kurmadan okuduğunda **0 satır** gördü — yani `FORCE`
salt tablo sahipliğini yener. Ama `tappa_owner` initdb'nin bootstrap
superuser'ıdır ve **superuser RLS'i koşulsuz atlar**; `FORCE` ona erişemez.

| rol / koşul | RLS'e tabi mi? |
|---|---|
| superuser (`tappa_owner`) | **Hayır** — koşulsuz atlar, `FORCE` erişemez |
| `BYPASSRLS` rolü | **Hayır** |
| tablo sahibi, `FORCE` **yok** | **Hayır** (Postgres varsayılanı) |
| tablo sahibi, `FORCE` **var** | **Evet** — `FORCE` sahipliği yener |
| `tappa_app` (NOSUPERUSER, NOBYPASSRLS, sahip değil) | **Evet** |

**Normatif sonuç:** CLAUDE.md §8'in zorunlu kıldığı RLS izolasyon testi
`tappa_app` ile — yani `DATABASE_URL` havuzuyla — koşmak **zorundadır**.
`tappa_owner` ile koşan bir izolasyon testi RLS'i **hiç** sınamaz; üstelik
sessizce değil **gürültülü** geçer (owner satırları görür, testin negatif
assertion'ı patlar) — tehlike "yanlış güven" değil, bozukluğun yanlış yerde
(RLS'te) aranmasıdır; oysa roldedir. Bu yüzden M1-09 testinin içinde
`SELECT current_user` = `tappa_app` **ve** `rolsuper/rolbypassrls = f,f` açıkça
doğrulanır — havuzu "appPool" diye **adlandırmak** kanıt sayılmaz.

### İzolasyon testi ≠ üretim sorgusu (normatif ayrım)

§4.5 **üretim** sorgularının RLS'e ek olarak açık `tenant_id` filtresi taşımasını
zorunlu kılar. Ama **izolasyon testi** bu filtreyi taşımamalıdır: taşırsa 0
satırın sebebi RLS değil `WHERE`'dir ve test RLS kapalıyken de yeşil kalır. Aynı
iddia ("bağlam B iken A'nın `id=1` satırı okunamaz, 0 beklenir") iki sorgu
şeklinde ölçüldü:

| sorgu şekli | `tappa_app` | `tappa_owner` | vaka ne ölçer |
|---|---|---|---|
| ham: `ctx=B`, `WHERE id=1` | **0** ✅ | 1 ❌ | RLS gerçekten sınanır |
| §4.5 store biçimi: `ctx=B`, `WHERE id=1 AND tenant_id=<B>` | **0** ✅ | **0** ✅ | iki rolde de geçer — RLS sınanmaz |

İkisi çelişki değil, **farklı iştir**: üretim sorgusu filtreyi taşımak zorundadır
(§4.5), izolasyon testi ise RLS'in yalın işini ölçmek zorundadır (§8). Filtreli
biçim ayrı bir vaka olarak durabilir ama izolasyon kanıtı **sayılmaz**
(M1-09'da `TestStoreQueryFiltersByTenant` gibi ayrı adlandırılır).

## Sonuçlar

- **[M1-02](../plan/m1-veri-katmani.md)…M1-06, M1-11:** her tenant politikası
  Karar madde 3'teki `NULLIF(...)` biçimini **birebir** yazar (`tenants` `id`
  üzerinden, diğerleri `tenant_id` üzerinden). Her tablo CLAUDE.md §6 beşlisiyle
  doğar: `tenant_id NOT NULL` · `tenant_id` indeksi · `ENABLE`+`FORCE ROW LEVEL
  SECURITY` · hem `USING` hem `WITH CHECK` politikası · `tappa_app` GRANT.
  `scripts/redline-check.sh` R5 bunu tarar (tarama geçmesi ihlal olmadığını
  kanıtlamaz — §4).
- **[M1-04](../plan/m1-veri-katmani.md) (sessions), M1-05 (tags):** Karar madde 7
  için bağlamsız, **anahtar-kısıtlı** erişim kurulur; genel bypass açılmaz.
- **[M1-07](../plan/m1-veri-katmani.md):** `WithTenant`
  `set_config('app.tenant_id', $1, true)`'i kendisi kurar; `LOCAL`'siz `SET`
  hiçbir yerde geçmez. Bu, `NULLIF`'in "sessiz 0 satır" bedelinin telafisidir.
- **[M1-08](../plan/m1-veri-katmani.md):** `GetTagByUID` ve
  `GetEmployeeBySessionHash` `db/queries/resolve.sql`'de durur; kalan tüm sorgular
  açık `tenant_id = @tenant_id` filtresi taşır. *(M5-02 aynı dosyaya üçüncüyü —
  `GetInviteByCodeHash` —, M6-01 A fazı dördüncü ve beşinciyi —
  `GetAdminByEmail`, `GetAdminSessionByTokenHash` — ekledi; **güncel liste madde
  7'dedir.**)*
- **[M1-09](../plan/m1-veri-katmani.md):** izolasyon vakaları (1, 2, 3, 7)
  `tappa_app`/`DATABASE_URL` havuzuyla ve **ham** (filtresiz) sorguyla koşar;
  vaka 5 (`tappa_owner` `DELETE` → trigger) owner havuzuyla; her negatif vakaya
  pozitif kontrol eşlik eder (koruma kapatılınca kırmızıya döner).
- **[M3-05](../plan/m3-policy-motoru.md):** `sys:tenant-mismatch` guardrail'i
  (sıra 1) çözümleme sonrası tenant ayrışmasını keser.
- **[CLAUDE.md](../../CLAUDE.md) §6** bu ADR ile tutarlıdır: politika ifadesi
  (`NULLIF`) ve "izolasyon testi ile üretim sorgusu farklı şekiller ister"
  ayrımı orada zaten güncel.
- Bu ADR yazılmadan hiçbir M1 migration'ı başlamamalıydı; artık M1-02 buna
  referansla yazılabilir.

## Değerlendirilen alternatifler

| Alternatif | Neden seçilmedi |
|---|---|
| **Şema-per-tenant** (her tenant ayrı Postgres şeması) | Ölçekte şema/migration çoğaltması operasyon yükü; çapraz-tenant çözümleme (madde 7) ve tek `transactions` raporlaması zorlaşır; tek VPS'te şema/bağlantı patlaması. RLS tek şemada aynı izolasyonu daha ucuz verir. |
| **Yalnız uygulama filtresi** (RLS yok, her sorguda `WHERE tenant_id=`) | Tek bir unutulan `WHERE` sessiz çapraz-tenant sızıntısıdır; §4.5 izolasyonu "her katmanda" ister. Filtre kalır ama RLS'in **yanına** (kuşak+kemer), tek savunma olarak değil. |
| **Çıplak cast** `current_setting('app.tenant_id', true)::uuid` | Fail-closed ama **belirsiz**: GUC'a ilk yazmadan sonra bağlamsız sorgu `0 satır` değil `ERROR` verir (Q27 tablosu). Davranış bağlantı geçmişine bağlı; taze bağlantıya yazılmış test geçer, üretim patlar. `NULLIF` determinizm için seçildi. Çıplağın tek üstünlüğü (gürültülü patlama) M1-07 `WithTenant` ile telafi edildi. |
| **`tappa_app`'e `BYPASSRLS`** (çözümlemeyi kolaylaştırmak için) | Tüm izolasyonu tek satırda iptal eder; Karar madde 6/7 bunu açıkça yasaklar. Çözümleme dar, anahtar-kısıtlı istisnayla halledilir. |
| **Superuser `tappa_owner`'a ait `SECURITY DEFINER` çözümleme fonksiyonu** | `SECURITY DEFINER` gövdesi sahibinin yetkisiyle koşar; sahip superuser olduğu için gövde RLS'i **tümüyle** atlar → tüm veritabanını kapsayan genel bir bypass, patlama yarıçapı sınırsız. Dönüşün sınırlı kalması bir *gövde özelliği*dir, ayrıcalık modelinin yapısal garantisi değil. Çözümleme, en-az-ayrıcalıklı (yalnız çözümleme tablolarına — `sessions`, `tags`, `employee_invites`, `admin_users`, `admin_sessions` — sütun-düzeyinde erişen) bir arayüzün ardında yapılır (Karar madde 7). |
| **GUC-anahtar saf-RLS** (`app.resolve_token` GUC'una anahtarlanmış, toplamsal bir RLS dalı; **bypass yok**) | Mutlu-yolu **bypass'sız** ifade eder ve "saf RLS bunu ifade edemez" iddiasını çürütür (canlıda doğrulandı: `tappa_app` tam RLS'e tabi, dönüş tek anahtarla sınırlı). Reddedilme sebebi ifade gücü değil, çevrelemenin **yapısal olmaması**: iki tek-nokta hatası canlıda çapraz-tenant ihlali üretti — (1) **okuma:** `SET LOCAL`'sız kurulan resolve-GUC havuz bağlantısında kalır, OR-dalı o bağlantıdaki her meşru sorguya bir çapraz-tenant satır sızdırır (`NULLIF` fail-closed yakalayamaz, `WithTenant` resolve-GUC'a dokunmaz); (2) **yazma:** `FOR ALL USING(...)` kısayolu `WITH CHECK`'i varsayılan olarak `USING`'den kopyalar → çapraz-tenant `INSERT` forge. İkisi de yalnız **disiplinle** engellenir; §4.5 yapısal çevreleme ister. Bu yüzden en-az-ayrıcalıklı `tappa_resolver` + anahtar-kısıtlı `SECURITY DEFINER` fonksiyon seçildi (Karar madde 7). |
| **RLS testini `tappa_owner` ile koşmak** (tek havuz) | `tappa_owner` superuser'dır ve RLS'i koşulsuz atlar (M0-03 tablosu); testin negatif vakaları gürültülü patlar. İzolasyon `tappa_app` ile ölçülmek **zorundadır**. |
