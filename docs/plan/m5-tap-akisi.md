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
- Doğrulama **tek sorgu** (`GetEmployeeBySessionHash`); zamanlama yüzeyi
  gerekçelendirilmiş ve Go tarafında sır karşılaştırması yok (aşağıdaki
  düzeltme bloğu).
- Deaktive çalışanın oturumu **anında geçersiz kılınabilir**: iptal yüzeyi
  (`RevokeSession` / `RevokeSessionsForEmployee`) var, iptal edilmiş oturum
  sonraki doğrulamada **düşer** — ama *deaktiflik kararı* session katmanında
  DEĞİL, `sys:employee-deactivated` guardrail'inde kalır (§5 satır 4: kayıt
  yazılır). Gerekçe aşağıdaki düzeltme bloğunda.
- **Q11 — kodla karşılanmaz, AÇIK kalır.** Sunucu tarafı (uzun `Max-Age`,
  `httpOnly`, `Secure`, `SameSite=Lax`) M5-01'de yazıldı ve doğrulandı; gerçek
  bir iPhone'da NFC → Safari akışıyla çerez ömrünün ölçülmesi **kullanıcının
  eylemidir** ve M5-01'i bloklamaz. Ölçüm yapılınca sonucu
  `open-questions.md`'ye yazılır. Ölçüm kod tasarımını değiştirmez; değiştirirse
  ayrı bir görev açılır.

> **Kart düzeltmesi (2026-07-31, M5-01 uygulaması sırasında).** Üç kriter
> gerçekle çelişiyordu; üçü de **zayıflatılmadan** yeniden yazıldı.
>
> **1) "Tek sorgu" ile "deaktive oturum anında geçersiz" aynı anda tutulamıyor —
> ve tutulmaya çalışılırsa §5 İHLAL EDİLİR.** Ölçüm:
> [`00003`](../../db/migrations/00003_create_employees_sessions.sql) satır
> 189-205'teki `resolve_session_by_token_hash` yalnızca
> `(id, tenant_id, employee_id, revoked_at)` döndürür — **`employees.status`
> yok**. Yani o tek sorgu deaktifliği göremez; görebilmesi için ya ikinci bir
> sorgu ya uygulanmış migration'ın değiştirilmesi gerekirdi (§6: yasak).
>
> Asıl mesele performans değil, **doğruluk**: session katmanı deaktif bir
> oturumu sessizce "oturum yok"a çevirirse, §5 **satır 3** (aktivasyon sayfası,
> **kayıt YAZILMAZ**) devreye girer ve deaktive çalışanın denemesi **iz
> bırakmadan kaybolur**. Oysa §5 **satır 4** o denemeyi `reject` + log +
> güvenlik uyarısı olarak **kayda geçirmeyi** emreder (§4.6 "kayıt asla
> kaybolmaz"). Karar-yeri hatası bir kırmızı çizgi ihlaline dönüşürdü.
>
> **Doğru sınır: session katmanı gerçeği taşır, karar vermez** — 00003'ün
> `revoked_at` yorumundaki ("iptal kararı domain katmanındadır") ilkeyle birebir
> aynı. Deaktiflik otoritesi zaten `internal/policy`'nin `sys:employee-deactivated`
> guardrail'i + `tap.Decide`'dır (M4'te bitti; `tap.Input.Employee` nil ile
> `deactivated`'ı bilerek ayırır).
>
> **Düşürülen garanti neyle karşılanıyor** (M0-02 2. tur dersi — zayıflatma
> değil, yer değiştirme):
> 1. Session katmanı `revoked_at`'ı **doğrular**: iptal edilmiş oturum `Verify`
>    çağrısında `ErrRevoked` alır, `Touch` fail-closed düşer.
> 2. İptal yüzeyi **var ve testli**: `Revoke`, `RevokeSessionsForEmployee`
>    (bir çalışanın tüm canlı oturumları, idempotent). Meşru çağıranlar:
>    **çalınan/kaybolan telefon** ve **ikinci-aktivasyon kararı (M5-02)**.
>    ⚠️ **Deaktivasyon (M6-05) bunu ÇAĞIRMAZ** — gerekçe aşağıdaki 3. tur ekinde;
>    bu satır önce "M6-05 çağırır" diyordu ve 3. turda çürütüldü.
> 3. Tap yolunda otorite **guardrail**'dir: `employees.status` okunur, kayıt
>    yazılır, uyarı üretilir. Bu yüzden `Verify` iptal durumunda bile
>    **çözümlenen gerçekleri döndürür** (`Resolved` dolu + `ErrRevoked`) —
>    çağıran kaydı yazabilsin diye. Kayıt kaybı yolu yapısal olarak kapalı.
>
> **2) "sabit zamanlı" kriteri, olmayan bir karşılaştırmayı istiyordu.** Ana
> arama Postgres'te `sessions.token_hash` UNIQUE indeksi üzerinden yapılır ve bu
> **sabit zamanlı değildir**. Kabul edilebilir olmasının gerekçesi yazıldı
> (`internal/session/manager.go`, `Verify` doküman bloğu): aranan değer, sunucu
> tarafındaki anahtarla üretilmiş bir **HMAC-SHA256 çıktısıdır**. Zamanlama
> sinyalini gerçek bir satıra doğru yürütmek için saldırganın *hash*'i seçmesi
> gerekir; hash'i seçmek HMAC'i hesaplamayı, o da anahtarı bilmeyi gerektirir.
> Çerez değerini serbestçe seçebilir, karşılık gelen hash'i seçemez. Ayrıca
> komşu hash'ler sızsa bile işe yaramaz: çerez bir **ön-görüntü** taşımak
> zorundadır. Go tarafında bu pakette **hiçbir sır karşılaştırması yoktur**
> (eşleştirmeyi DB yapar); eklenecek olursa `crypto/subtle.ConstantTimeCompare`
> zorunludur (redline R7) — `==` veya `bytes.Equal` değil.
>
> **3) "Q11 ölçüldü" bir kabul kriteri olamaz.** Kriter, M5-01'in ancak gerçek
> bir iPhone'la kapanabileceğini söylüyordu; ölçüm **kullanıcının eylemidir**,
> yapıcı ajanın erişemeyeceği bir donanım ister ve kod tasarımını
> değiştirmez — sunucu tarafı zaten `Set-Cookie` + `httpOnly` + `Secure` +
> `SameSite=Lax` + ~1 yıl `Max-Age` yazıp test eder. Kriter, M5-01'in gerçekten
> teslim ettiğini söyleyecek ve Q11'i **açık** bırakacak biçimde yeniden yazıldı.
> `open-questions.md`'ye **dokunulmadı** (orkestratörün).
>
> **Dokunulmayan:** Cihaz limiti kart gereği opsiyonel bırakıldı — M5-01 yalnızca
> sorgu yüzeyini (`ListForEmployee`, `RevokeSessionsForEmployee`) verir, politika
> uygulamaz.
>
> **3. tur eki (aynı gün, `tappa-security-auditor` RED sonrası).** Denetim,
> 2. turdakiyle **aynı sınıftan** bir hata buldu: dosya, ölçmediği bir garantiyi
> yorum olarak beyan ediyordu. `cookie.go` "hiçbir çağıran prod'da Secure'suz
> çerez üretemez" diyordu; **yanlıştı.** `Cookies` alanı `secure bool` olduğu için
> **sıfır değer = Secure'suz** demekti ve Go, başka bir paketin unexported alanı
> *adlandırmasını* yasaklar, sıfır değeri **elde etmesini** değil:
> `var c session.Cookies`, `session.Cookies{}` ve — asıl tehlikelisi — bir handler
> struct'ında **alanı hiç yazmamak** (Go'da derleme hatası değildir). Üçü de
> ölçüldü, üçü de `HttpOnly; SameSite=Lax` yazıp **`Secure` yazmıyordu**.
>
> **Düzeltme — alanın kutbu çevrildi:** `insecure bool`, `Secure() = !insecure`.
> Artık **sıfır değer fail-closed (Secure)** ve gevşemeyi yalnız `NewCookies`
> isteyebilir (tek dal: prod DEĞİL **ve** BaseURL https DEĞİL). "Set/Clear
> kurulmamış codec'i reddetsin" alternatifi yerine kutup çevirme seçildi: sentinel
> alan, hata döndürmeyen bir metotta hata yolu ve disiplin gerektirmiyor —
> tehlikeli durum **kazara temsil edilemez** hâle geliyor. Regresyon testi harici
> test paketinden yazıldı (üç sıfır-değer yolu + dev/http'nin hâlâ gevşediğini
> gösteren pozitif kontrol).
>
> **Bu turda düzeltilen üç şey daha:** (1) `Token` karşılaştırması artık **kimlik**,
> değer değil (`*string` sonrası aynı değeri saran iki Token `!=`) — yorum gerçeğe
> indirildi ve teste bağlandı; sonuç fail-closed, açık değil. (2) **Deaktivasyon
> oturum iptal ETMEMELİ**: `sys:employee-deactivated` yalnız `employees.status`'e
> bakar, iptal reddetmeye hiçbir şey katmaz ama her sonraki tap'i `ErrRevoked`
> dalına iterek §4.6 kayıp koşulunu üretir. `RevokeAllForEmployee`'nin meşru
> çağıranları çalınan/kaybolan telefon ve M5-02 ikinci-aktivasyon;
> belge tersine çevrildi (kod değişmedi). (3) `IssueParams.DeviceInfo` **sınırlandı**
> (≤64 rune, geçerli UTF-8, Unicode `Cc` **ve** `Cf` reddedilir — U+0085 NEL ve
> U+202E RTL-override dahil —, **kırpma değil RED** — §7 sessiz kabul yasağı;
> CSV formül enjeksiyonu ve HTML kaçışı bilinçli olarak **bu katmanın işi değil**,
> sırasıyla M6-07 CSV yazıcısı ve templ şablonu);
> gerçek UA'lar reddediliyor, meşru kaba etiketler dokunulmadan geçiyor.
>
> **Kapsam genişlemesi (agent-brief madde 8):** `TAPPA_ENV` artık **kapalı kümede**
> doğrulanıyor (`internal/config`, M5-01 kapsamı dışı). Gerekçe: bu değer
> `IsProd()` üzerinden çerez sertleşmesini seçtiği için artık bir güvenlik
> nitelidir; `TAPPA_ENV=production` yazımı `IsProd()`'u sessizce false yapardı.
> Repodaki tüm mevcut değerler tarandı (`.env`, `.env.example` = `dev`; CI hiç
> set etmiyor → varsayılan `dev`), hiçbiri kırılmadı.
>
> **2. tur eki (aynı gün, üçüncü göz RED sonrası).** Bağımsız denetim, bu kartın
> "ham token hiçbir yerde yok" kriterinin ilk uygulamada **karşılanmadığını**
> ölçtü: `session.Token`'ı **unexported** bir alanda taşıyan sıradan bir
> handler struct'ı `%v`/`%+v`/`%#v`/`slog` ile ham değeri basıyordu (`fmt`,
> `CanInterface()==false` olduğu için `Formatter`/`Stringer`'ı atlar). Düzeltme:
> değer **dolaylı** tutuluyor (`*string`) → aynı yol artık adres basıyor.
> Regresyon testi **harici bir test paketinden** (`package session_test`)
> yazıldı, çünkü hata tam da paket sınırının dışında ortaya çıkıyor. Kalan sınır
> açıkça yazıldı: kasıtlı `reflect` okuması hâlâ mümkün ve bu "imkânsız" diye
> belgelenmiyor.

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
- **Kod entropisi ≥128 bit — DEĞİLSE katı kilitleme ZORUNLU.** Şema (00009) hiçbir
  kilitleme durumu taşımaz ve bu yalnızca ≥128-bit kodla savunulabilir. Kod bundan
  kısaysa (insan okuyan/yazan her biçim dahil) şunlar **birlikte** gerekir: davet
  ve kaynak IP bazlı **deneme sayacı + kilit damgası** (yeni migration), **kısa
  TTL**, başarısız her denemenin **`audit_log`'a** yazılması, `/api/activate`'e
  **kendi dar oran sınırı** (M5-03'ün geniş tap sınırı kapsamaz).
  ⚠️ **Bunu yakalayacak mekanik bir kontrol YOKTUR:** `code_hash` CHECK'i hash'in
  **şeklini** sınar, kodun entropisini değil — 6 haneli bir kodun hash'i de
  64-hex'tir (ölçüldü). Yani bu kriter yalnızca **okunarak** tutulur; kısa koda
  geçen değişiklik bu maddeyi karşıladığını raporunda **göstermek zorundadır**.
- **Aktivasyon ⇒ tüketim.** Bir çalışanı `active` yapan tek veri-katmanı yolu
  `ConsumeInviteAndActivate`'tir (tek ifade: davet tüketilmeden aktivasyon olmaz).
  B fazı ayrı bir "aktive et" sorgusu **eklemez**; gerekiyorsa (M6 müdür eylemi)
  ayrı bir görevde, ayrı gerekçeyle gelir.
- Zaten aktif çalışanın ikinci aktivasyonu: yeni cihaz mı, saldırı mı — davranış
  belirlenmiş ve testli.
- §5 satır 3'ün **hedefi** buraya düşer: aktivasyon sayfası var, erişilebilir ve
  hiçbir **`transactions`** satırı yazmıyor (§5 satır 3'ün yasakladığı budur).
  ⚠️ "Bir kod harcanana kadar hiçbir kayıt yazmıyor" **değil**: tüketilmiş bir
  kodla gelen `GET /activate` bir `audit_log` satırı yazar ve bu **doğrudur**
  (§4.6 reddedilen denemenin iz bırakmasını ister) — yanlış olan eski cümleydi,
  davranış değil. ⚠️ **Yönlendirmenin kendisi
  M5-02'nin işi DEĞİL:** "oturum yoksa tap → aktivasyon sayfası" dalını kuran uç
  nokta `GET /t`'dir ve o **M5-04**'tür. Bu satır önce "buraya bağlanır" diyordu;
  M5-02 bittiğinde bağlanamayacağı ölçüldü (tap uç noktası yok) ve kriter gerçeğe
  indirildi — düşen bir garanti yok, yalnızca sahibi doğru göreve yazıldı.
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

> **Kart düzeltmesi (2026-07-31, M5-02 A fazı sırasında).** Görev **iki faza**
> bölündü (orkestratör kararı) ve kart bunu söylemiyordu; bölünme kriterleri
> **değiştirmez**, yalnızca hangi fazın hangisini kapattığını sabitler.
>
> **A fazı (bitti) — yalnız veri katmanı.** Migration
> [00009](../../db/migrations/00009_create_employee_invites.sql)
> (`employee_invites` + RLS beşlisi + `REVOKE DELETE` + çözümleme fonksiyonu),
> [`db/queries/invites.sql`](../../db/queries/invites.sql) (`CreateInvite`,
> `ConsumeInviteAndActivate`, `ListPendingInvitesForEmployee` — DELETE sorgusu
> **yok**, consume-by-id sorgusu **yok**, ayrı "aktive et" sorgusu da **yok**),
> `internal/db/resolve.go` → `GetInviteByCodeHash`, testler
> `internal/db/invites_test.go` + `rls_test.go`.
>
> **B fazı (açık) — kartın geri kalanı aynen geçerli:** `POST /api/activate`,
> `Set-Cookie`, aktivasyon sayfası, **GDPR Art. 13 metni**, **WiFi adımı (Q14)**,
> §5 satır 3 bağlantısı, uç nokta oran sınırı, kodun üretimi/HMAC'i ve Q02 kanalı.
> Hiçbiri A fazında yapılmadı ve hiçbiri kriterlerden düşürülmedi.
>
> **A fazında bağlanan beş karar** (B fazını bağlar):
>
> 1. **Entropi dalı seçildi: ≥128 bit — ama bunu ŞEMA ZORLAMIYOR, kabul kriteri
>    zorluyor.** Tablo bilerek **hiçbir kilitleme durumu taşımıyor** (deneme sayacı /
>    `locked_until` / IP sütunu yok); bu ancak kriptografik ≥128-bit kod
>    varsayımıyla doğrudur. `code_hash` bir **biçim CHECK'i** aldı
>    (`^[0-9a-f]{64}$` — `internal/session`'ın ürettiği hex HMAC-SHA256 şekli) ve
>    bunun satın aldığı şey **yalnızca şudur: ham, hash'lenmemiş bir kodu sütuna
>    yazma KAZASI** artık veritabanı tarafından reddediliyor (insan-okuyan kod,
>    base64url token, büyük harfli hex, yanlış uzunluk → 23514, ölçüldü).
>    **CHECK entropiyi ÖLÇMEZ** — 6 haneli bir kodun hash'i de tam 64-hex'tir
>    (ölçüldü: `encode(sha256('123456'::bytea),'hex') ~ '^[0-9a-f]{64}$'` → `t`).
>    Yani B fazı kısa bir koda geçip onu düzgünce HMAC'lerse **CHECK yeşil kalır,
>    hiçbir migration gerekmez ve hiçbir mekanik uyarı ateşlenmez.** Bu yüzden
>    yükümlülük yukarıdaki **kabul kriterine** yazıldı; buradaki blok onu
>    hatırlatır, zorlamaz. (CHECK'in kendisi
>    `sessions.token_hash`/`password_resets.token_hash` precedent'inden **bilinçli
>    sapmadır** — gerekçe: iddiayı yazan dosya onu zorlamak zorundadır.) Uç nokta
>    oran sınırı ve başarısız denemenin `audit_log`'a yazılması her iki dalda da
>    **B fazının işi** — şema değil.
> 2. **Tüketim KOD HASH'i ile anahtarlanır (`id` ile DEĞİL) ve aktivasyonla TEK
>    İFADEDE birleşiktir** (iki denetim bulgusu, tek çözüm).
>    `ListPendingInvitesForEmployee` davet `id`'sini tenant içindeki her çağırana
>    verir; id ile tüketen bir sorgu **kodu bilmeyi gereksiz** kılardı. Ayrı bir
>    `ActivateEmployee` sorgusu ise "aktive edildi ⇒ bir davet tüketildi"
>    çıkarımını **çağrı sırasına** bırakıyordu: tenant bağlamındaki bir kod yolu
>    hiç davet harcamadan istediği çalışanı aktive edebiliyordu (ölçüldü).
>    İkisi de **silindi**; yerlerine tek atomik ifade geldi:
>    `ConsumeInviteAndActivate` (data-modifying CTE — tüket, yalnız tüketildiyse
>    aktive et). Bu katmanda `active` olmanın **başka yolu yok**.
>    **Deaktif çalışanda davet YANMIYOR — koruma yapısal.** İlk uygulamada
>    veri-değiştiren CTE koşulsuz çalıştığı için `deactivated` çalışanda damga
>    yazılıyor, aktivasyon olmuyordu; davet yalnızca çağıran hatayı ilettiği için
>    (`WithTenant` rollback) kurtuluyordu — yani koruma **disiplindi**. Ölçüldü:
>    guard'sız ifade COMMIT ile daveti **yakıyor** (`used_at` dolu), guard'lı ifade
>    **yakmıyor** (`used_at` NULL). Düzeltme: iç CTE'nin `WHERE`'ine
>    `AND EXISTS (… employees … status IN ('invited','active'))`. Artık hatayı
>    yutup COMMIT eden dikkatsiz çağıran bile daveti harcayamıyor (test tam da
>    bunu simüle ediyor: hata yutuluyor, transaction commit ediliyor, davet hâlâ
>    tüketilmemiş ve sonradan başarıyla harcanabiliyor).
>    **Çift kontrol bilinçli:** CTE'deki `EXISTS` **damgayı**, dıştaki
>    `status IN (…)` **aktivasyonu** korur; ikincisi bir YARIŞ koruyucusudur
>    (READ COMMITTED yeniden değerlendirmesi), tekrar değil.
>    **Kalan pencere yazıldı (ölçüldü):** deaktivasyon tam da CTE ile dış UPDATE
>    arasında commit ederse damga yazılır ve commit eden çağıran kodu boşa harcar
>    — ama **aktivasyon yine olmaz** (fail-closed); en kötü sonuç yanmış bir kod,
>    yetkisiz aktivasyon değil. Pencereyi kapatmak için `EXISTS (… FOR SHARE)`
>    denendi ve **ölçülerek reddedildi**: aynı çalışanın iki eşzamanlı
>    aktivasyonunu kilit yükseltmesinde **deadlock**'a sokuyor
>    (`ERROR: deadlock detected`, 40P01).
>    Panelden **iptal** ayrı bir işlemdir ve `used_at`'i yeniden kullanamaz
>    ("kod kullanıldı" demektir); M6 gerektirdiğinde kendi sütununu yeni bir
>    migration'da alır. Akış: resolve (bağlamsız) → `WithTenant` → tek ifade;
>    açık `tenant_id` yüklemi **her iki yarıda da** korundu (§4.5 kuşak+kemer).
> 3. **"Kullanıldıktan sonra ölü" = damga, silme DEĞİL.** Tüketim `used_at`
>    damgasıdır; satır **uygulama tarafından silinemez** (`REVOKE DELETE`, ampirik:
>    `has_table_privilege('tappa_app','employee_invites','DELETE')` = `f`), çünkü
>    "hangi kod bu kişiyi ne zaman aktive etti" bir denetim izidir (§4.6).
>    **Garantinin kapsamı yazıldı:** bu `tappa_app`'i bağlar, `tappa_owner`'ı
>    (migration superuser) bağlamaz — onu da kapatmak 00005'teki gibi bir trigger
>    ister, ama 00005'in trigger'ı UPDATE'i de yasakladığı için burada
>    kullanılamaz (`used_at` yazılabilir kalmalı); bilinçli non-goal,
>    `sessions`/`employees` precedent'iyle aynı.
>    **UPDATE artık SÜTUN düzeyinde:** `GRANT UPDATE (used_at)` — tablo geneli
>    UPDATE, dosyanın hiç anmadığı iki yazmaya daha izin veriyordu (süresi geçmiş
>    daveti `expires_at`'i ileri alarak **diriltmek**, `employee_id`'yi aynı tenant
>    içinde **başka çalışana kaydırmak**); üçü de artık `permission denied`
>    (ölçüldü). `sessions`/`tags`'ten bilinçli sapma — orada birden çok meşru
>    yazılabilir sütun var, burada tek. **Kalan sınır:** sütun grant'ı hangi
>    sütunun yazılacağını sınırlar, hangi DEĞERİN yazılacağını değil —
>    `used_at := NULL` (un-consume) hâlâ yetki düzeyinde mümkündür; onu tutan tek
>    şey sorgu disiplinidir, yapısal olması trigger ister (yeni migration).
>    Tek-kullanımlık **atomiktir** (`ConsumeInviteAndActivate`, §4.4 kalıbı);
>    50 goroutine → tam 1 kazanan, ve testin boş yere yeşil olmadığını gösteren
>    **negatif kontrol** aynı dosyada (oku-sonra-yaz 8 kazanan üretiyor).
> 4. **İkinci aktivasyon: veri katmanı karar VERMEZ, ama bir şeyi yapısal olarak
>    kapatır.** `ConsumeInviteAndActivate` `status IN ('invited','active')` ister:
>    zaten aktif çalışanın ikinci aktivasyonu bu katmanda hata değildir (kararı B
>    fazı verir), ama **`deactivated` çalışan bir davetle geri aktive edilemez** —
>    aksi halde müdürün deaktivasyon kararı bir linkle dolanılabilirdi. Ayrıca
>    `activated_at` **ezilmez** (`COALESCE`), ilk aktivasyon damgası korunur.
>
> 5. **"Süresiz davet" iki türlü yazılır; ikisi de kapatıldı, üçüncüsü sınır
>    olarak yazıldı.** `expires_at NOT NULL` yalnız **atlanan** süreyi engeller;
>    ilk taslakta `expires_at = 'infinity'` sorunsuz insert oluyordu ve
>    `now() < 'infinity'` sonsuza dek doğru — yani gerçekten ölümsüz bir davet
>    (ölçüldü, varsayılmadı). `CHECK (expires_at <> 'infinity')` eklendi.
>    **Hâlâ yazılabilen:** sonlu ama saçma bir tarih (9999). O bir **TTL politikası**
>    sorusudur ve TTL kararı B fazınındır; B fazı üst sınırı yapısal isterse **yeni
>    bir migration**'da sınırlı bir CHECK ekler. `-infinity` bilerek serbest: "zaten
>    süresi dolmuş" demek, fail-closed. Test: `TestEmployeeInvites_ExpiryCannotBeAbsent`.
>
> **Kart dışı yan etkiler (kapsam işaretleri):**
> 1. Çözümleme sorgusu sayısı 2 → 3 oldu, bu yüzden **ADR 0002 madde 7**
>    güncellendi (karar değişmedi; sayı garanti değil, beş normatif kısıt garanti
>    — üçüncüsü beşini de canlı katalogda karşılıyor).
> 2. **`scripts/redline-check.sh` R7 deseni genişletildi** (güvenlik denetçisi
>    bulgusu): `code[_a-z]*hash` ve yapılandırılmış log anahtarı `"code",` eklendi.
>    Gerekçe: bu tasarımda `code_hash` bir **taşıyıcı (bearer)** kimlik bilgisidir
>    (hash → resolver → tenant → tüketim), yani hash'i loglamak kodu loglamakla
>    aynı ağırlıktadır; eski desen bu satırları **kaçırıyordu** (ölçüldü).
>    **Desen KELİMEYİ değil DEĞERİ hedefler** — ilk deneme çıplak
>    `[^a-z]code[^a-z]` idi ve iki masum satırı (`"reason", "code expired"` ve
>    `fmt.Errorf("… invalid activation code: %w", …)`) FAIL veriyordu; bu, ağın
>    **aşınmasına** (baskı altında gevşetilmesine) davetiyeydi. Yeni desen:
>    dört gerçek sızıntı satırını yakalıyor, iki masum satırı **geçiriyor**, temiz
>    repoda `exit 0` (üçü de ölçüldü). Sınır: `"hash"`/`"c"` gibi bir anahtar
>    altında loglanan değer yakalanmaz (değişken adı `codeHash` ise yakalanır).

> **Kart düzeltmesi (2026-07-31, WiFi adımının veri katmanı).** Kartın WiFi
> maddesi "ağ adı **lokasyon kaydından** gösterilir" diyordu; `locations`
> tablosunda böyle bir alan **yoktu** — kart, şemanın karşılamadığı bir alanı
> varsayıyordu. Kullanıcı kararıyla alan eklendi:
> [00010](../../db/migrations/00010_add_wifi_ssid_to_locations.sql) →
> `locations.wifi_ssid text` (NULLABLE, `CHECK octet_length BETWEEN 1 AND 32`).
> Kriter değişmedi; yalnızca dayandığı alan artık gerçek.
>
> **B fazını bağlayan üç nokta:**
> 1. **Okuma yolu hazır:** `GetLocationWiFi` (`db/queries/locations.sql`) —
>    `tenant_id` + `id` ile, açık tenant yüklemiyle. Lokasyon id'si zaten
>    `ConsumeInviteAndActivate`'in döndürdüğü `e.location_id`'dir; B fazı yeni bir
>    sorgu yazmak zorunda değil.
> 2. **`wifi_ssid IS NULL` = "bu lokasyonun ağı yok, adımı atla"** — hata değil.
>    Boş dize veritabanı tarafından **reddediliyor** (23514), yani "ağ yok"un tek
>    yazımı NULL. Seed'de KF Msida bilerek NULL (nullable yolun fixture'ı).
> 3. **Sütun bir karar girdisi DEĞİL ve olmamalı:** hiçbir policy anahtarı,
>    `tap.Input` alanı veya güven puanı buna dallanmaz; IP kanıtı `static_ips`'ten
>    gelmeye devam eder. SSID istemci beyanıdır — bir hotspot'a istenen ad
>    verilebilir. Şifre/PSK, BSSID/MAC, sinyal gücü ve çevre ağ listesi bilinçli
>    olarak **eklenmedi** (§4.7 yeni sır yüzeyi; §4.1/§4.2 parmak izi ve arka plan
>    konum sinyali). Gerekçeler 00010'un içinde.
>
> **Sınır (yazıldı, iddia edilmedi):** bu üçüncü maddeyi zorlayan hiçbir mekanik
> kontrol yok — bir yorum kısıt değildir. Sütunu bir karar girdisine çevirmeyi
> engelleyen tek şey inceleme.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı sırasında).** Kriterlerin hiçbiri
> düşürülmedi; kartın *anmadığı* yedi karar aşağıda gerekçesiyle sabitleniyor.
> Kart "kod entropisi ≥128 bit" dalını istiyordu: **32 bayt (256 bit)**,
> `internal/invite/code.go` → `codeBytes`. Kısa/insan-okuyan koda geçilmedi, yani
> kilitleme migration'ı gerekmedi — ama şema bunu **zorlamıyor**, sabitin üstündeki
> yorum yükümlülüğü taşıyor (00009 zaten böyle söylüyordu).
>
> 1. **Alan ayrımı iki mekanizmayla: ayrı anahtar + etiketli türetme.** Brief
>    ikisinden birini istiyordu; ikisi de var, çünkü ikisi **farklı** şeye karşı
>    korur. `TAPPA_INVITE_HMAC_KEY` (yeni **zorunlu** env) kriptografik bağımsızlık
>    verir: birinin sızması diğerini forge edilebilir kılmaz. Etiket
>    (`HMAC(key, "tappa/invite/v1|" || code)`) **operasyonel** hatayı kapatır —
>    iki env'i tek `openssl rand` çıktısından kopyala-yapıştır etmek. `config.Load`
>    iki anahtarın **birebir eşitliğini** başlangıçta reddeder
>    (`crypto/subtle.ConstantTimeCompare`), ama eşitlik dışı bir bağımlılık
>    (ör. ikisini tek kaynaktan türeten gelecekteki bir yol) yalnız etiketle
>    zararsız kalır. **Ölçüldü:** aynı anahtar altında invite MAC'i, session'ın
>    çıplak `hex(HMAC(key, value))` yapısına **eşit değil**
>    (`TestHash_DomainSeparation`).
> 2. **Kod, sayfada DEĞİL kısa ömürlü HttpOnly çerezde taşınır.** `GET
>    /activate?code=…` kodu doğrular, `tappa_activation` çerezine yazar ve **temiz
>    `/activate`'e 303** döner. Kazanç ölçülebilir: (a) hiçbir **yanıt gövdesi**
>    kodu taşımaz (test her baytı tarıyor) — gizli input alanı kullansaydık kod,
>    ortak bir dükkân telefonunda render edilen HTML'de, geri-ileri önbelleğinde ve
>    "sayfayı kaydet" çıktısında olurdu; (b) ilk sıçramadan sonra **adres
>    çubuğunda/geçmişte** kod kalmaz; (c) `SameSite=Lax` sayesinde çapraz-site POST
>    çerezi taşımaz → **forge edilmiş POST** kapalı (çerez EKİMİ değil — bkz.
>    aşağıdaki 2. tur bloğu). **Bedeli açıkça yazıldı:**
>    çerezi reddeden tarayıcı aktive olamaz — ama ürün zaten kalıcı çereze
>    dayanıyor, bu yüzden hata **erken ve açıklanabilir** hâle gelir (plaket
>    başında değil). Çerez tipi `insecure bool` kutbuyla yazıldı (M5-01 3. tur
>    dersi): **sıfır değer Secure**.
> 3. **Üçüncü uç nokta: `GET /activate/done`.** POST 303 ile buraya yönlendirir ve
>    burası ziyaretçiyi **yeni oturum çerezinden** tanır. İki sebep: (a) PRG —
>    yenilenen sayfa tüketilmiş bir kodu tekrar POST etmez; (b) çerezin gerçekten
>    saklandığı **tam da o anda** ölçülür, ertesi gün plakette değil.
> 4. **İkinci aktivasyon: yeni cihaz kabul, önce iptal sonra üretim.** Zaten
>    `active` çalışan + geçerli kullanılmamış davet = yeni telefon.
>    `RevokeAllForEmployee` **`Issue`'dan ÖNCE** çağrılır — sıra taşıyıcıdır, çünkü
>    o sorgu çalışanın **tüm canlı** oturumlarını iptal eder ve sonra çağrılsaydı
>    yeni oturumu da öldürürdü (`TestSubmit_SecondDeviceRevokesBeforeIssuing`
>    sırayı ölçüyor). Aktivasyon öncesi form **uyarır** ("This is a new phone…"),
>    sonrası onay ekranı **söyler**, `audit_log`'a `activation.device_replaced`
>    yazılır. Gerekçe: çalınan telefon senaryosunda tek çare budur ve M5-01
>    `RevokeAllForEmployee`'nin meşru çağıranları arasında bunu zaten sayıyor.
>    ⚠️ Deaktivasyon bu yolu **çağırmıyor** (M5-01 3. tur eki) — dokunulmadı.
> 5. **Oran sınırı iki katman ve biri bilinçli olarak izsiz.** (a) **IP başına**,
>    DB'ye dokunmadan — yük atmanın tek mümkün yeri; tenant **bilinmediği** için
>    `audit_log`'a yazılamaz (tablo `tenant_id NOT NULL` + FK; "sistem tenant'ı"
>    uydurmak daha kötü olurdu). Bu boşluk yazıldı, kapatılmış gibi yapılmadı.
>    (b) **davet başına**, davet UUID'siyle anahtarlanır (kod/hash **asla** bellek
>    map'inin anahtarı olmaz) — tenant bilindiği için 429 **`audit_log` satırı
>    bırakır**. **Yalnız başarısızlıklar sayılır**, yani meşru akış (bir link, bir
>    form, bir onay) sınıra yapı gereği değmez — bu, "büyük sayı seçmek" yerine
>    ölçülen bir özellik (`TestRateLimit_SuccessNeverConsumesBudget`). M5-03'ün
>    geniş tap sınırı bu uçları **kapsamıyor**, kartın istediği gibi.
>    ⚠️ **Bu madde 4. turda ikiye bölündü ve bir cümlesi çürütüldü** — aşağıdaki
>    4. tur bloğuna bak: "meşru akış sınıra yapı gereği değmez" akışın **kendi
>    katkısı** için doğru, **servis edilip edilmediği** için değil.
> 6. **`TAPPA_RETENTION_YEARS` zorunlu, aralık [1,30].** Aralık **hukuki değil
>    mantık** sınırıdır (0 = "0 yıl saklanır" yazan bir aydınlatma metni; 500 =
>    "sonsuza dek"). Üretim dışı dağıtımlarda sayfa metni bunun **konfigürasyondan
>    okunan bir placeholder** olduğunu açıkça söylüyor (Q13 / backlog B3 hâlâ
>    açık). Kodda hiçbir yerde gömülü yıl sayısı yok.
> 7. **Q02 kanalı arayüz ardında ve iz bırakıyor.** `invite.Channel` tek egress;
>    tek implementasyon `ManagerVisibleChannel`, adı bilinçli olarak rahatsız edici
>    ve **her teslimde `invite.code_shown_to_manager` audit satırı** yazıyor —
>    ADR 0005 Y-D'nin tespit sinyali (M6-11) böylece ham veriye kavuşuyor.
>    `IssueAndDeliver` kodu **çağırana döndürmez**.
>
> **Kart dışı yan etkiler (kapsam işaretleri):**
> 1. **Yeni sorgu:** `db/queries/employees.sql` → `GetEmployeeActivationContext`
>    (isim + durum + lokasyon + **tenant adı** = GDPR veri sorumlusu). Kartın
>    "B fazı yeni sorgu yazmak zorunda değil" notu **yalnız SSID okuması** içindi;
>    SSID gerçekten `GetLocationWiFi` ile okunuyor. Bu dosyada **yazma sorgusu
>    yok** — A fazının "aktive eden tek ifade" garantisi korundu.
> 2. **Yeni sorgu:** `db/queries/audit.sql` → `RecordAuditEvent` (§4.6; tablo
>    vardı, INSERT sorgusu yoktu). Yeni paket `internal/audit`.
> 3. **Yeni paketler:** `internal/invite`, `internal/audit`, `internal/handler`
>    (ilk sakini), `web/templates/{layout,pages}` (ilk `.templ` dosyaları) →
>    `go.mod`'a `github.com/a-h/templ` eklendi, `make audit` koşuldu (M1-07→M1-09
>    dersi): `No vulnerabilities found`.
> 4. **`httpx.NewRouter` imzası değişti:** `NewRouter(cfg, features ...Mounter)`.
>    `cmd/tappa` artık havuzu ve yöneticileri kuruyor (wiring; iş mantığı yok).
>
> **Kapsam DIŞI bırakılanlar (bilinçli):**
> - **Davet üreten HTTP uç noktası YOK.** Panel M6, admin kimlik doğrulaması da
>   M6 — kimliksiz bir "davet üret" ucu, bu görevin azaltmaya çalıştığı Y-D
>   riskini doğrudan internete açardı. Davet bugün yalnız `invite.Manager` +
>   `Channel` üzerinden üretilebiliyor.
> - **M5-03 middleware'i yazılmadı:** istemci IP'si `r.RemoteAddr`'ın host'u,
>   `X-Forwarded-For` **okunmuyor**. Ters proxy arkasında IP penceresi tüm mekân
>   için ortak olur — bu yüzden IP penceresi geniş, davet penceresi dar.
> - **§5 satır 3 tam bağlanmadı:** yönlendirmeyi yapacak `GET /t` M5-04'te.
>   Teslim edilen, **hedefin var ve erişilebilir** olması: `/activate` çalışıyor ve
>   hiçbir `transactions` satırı yazmıyor. (Tüketilmiş kodla gelen bir istek
>   `audit_log`'a yazar — §4.6 gereği doğru; eski "hiçbir kayıt yazmıyor" cümlesi
>   3. turda ölçülerek çürütüldü.) "Bağlandı" denmiyor.
> - **M5-07 turu ve practice tap** bu görevde yok.
> - **Yazı tipleri hâlâ self-host EDİLMİYOR:** `web/static/fonts/` dizini yok,
>   `app.css`'te **sıfır** `@font-face` (ölçüldü) — M5-02 öncesinden gelen boşluk.
>   Kırmızı çizgi korunuyor — sayfa hiçbir dış kaynağa bağlanmıyor (render edilen
>   HTML'de tek `href` `/static/css/app.css`), ama Space Grotesk / IBM Plex Mono
>   gelene kadar tarayıcı **sistem yazı tipine düşer**. `base.templ` bir süre
>   tersini söylüyordu; iddia gerçeğe indirildi (2. tur). M5-04 ile kapanmalı.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı 2. tur — üçüncü göz RED sonrası).**
> Denetim iki **bloklayan** bulgu ölçtü; ikisi de aynı sınıftan: *dosya,
> sağlamadığı bir güvenlik sınırını beyan ediyordu.*
>
> **B1 — aktivasyon-fixation. "SameSite=Lax sayesinde CSRF yapısal olarak kapalı"
> YANLIŞTI ve çapraz-tenant render ÖLÇÜLDÜ.** SameSite çerez **yazmayı**
> kısıtlamaz, yalnız **göndermeyi**. Denetçi kurbanın tarayıcısını çapraz-site
> `GET /activate?code=<saldırganın kodu>` yaptırdı → `Set-Cookie` yerleşti →
> sonraki aynı-site GET **başka tenant'ın** formunu render etti → aynı-site POST
> (Lax bunu **taşır**) kurbanın telefonunu yabancı bir çalışan kaydına bağladı ve
> **kurbanın oturum çerezini sessizce ezdi**. Saldırı bu turda **yeniden üretildi**
> (üç adım da başarılı), sonra üç önlem eklendi ve **yeniden koşuldu** (üç adım da
> başarısız) — `internal/handler/activate_test.go` → `TestB1_*`:
>
> 1. **Synchronizer token.** Aktivasyon çerezi artık `<csrf>.<code>`; form
>    token'ı echo ediyor, `Submit` `crypto/subtle.ConstantTimeCompare` ile
>    karşılaştırıyor. Token **koddan türetilmiyor** (türetilseydi saldırgan
>    hesaplardı) ve sunucuda üretildiği için saldırgan onu **okuyamaz**. Geriye
>    kalan **kimlik avıdır**, CSRF değil — ve o sayfada **başkasının adı** yazar.
> 2. **`Submit` mevcut oturumu görüyor.** İstek canlı bir `tappa_session`
>    taşıyorsa ve aktive edilecek çalışan **o oturumun çalışanı değilse**: form
>    **409** ile geri döner, mevcut sahip **adıyla** yazılır ve ayrı bir onay
>    kutusu (`switch`) istenir. Sessiz ezme kalktı — kurbanın oturumu bir
>    **kanıttır**. `audit_log`'a `activation.blocked` düşer.
> 3. **Çapraz-site GET, dolu telefonda çerez ekemiyor.** `Sec-Fetch-Site:
>    cross-site` **ve** telefonda başka birinin canlı oturumu varsa çerez
>    **yazılmıyor**; aynı-site bir onay adımı (`POST /activate`, Origin kontrolü
>    **katı**) gerekiyor. ⚠️ **Kasıtlı ve ölçülmüş sapma:** koordinatör
>    "çapraz-site GET'te çerez ekme" dedi; **koşulsuz** uygulanmadı, çünkü ürünün
>    **normal** akışı (WhatsApp/SMS/e-postadaki linke dokunmak) **daima
>    çapraz-site**'tir — koşulsuz interstitial her aktivasyona bir tık ekler **ve**
>    kodu her ilk sayfanın gövdesine sokardı (denetimin doğruladığı "gövdede kod
>    yok" özelliğini bozardı). Kural bu yüzden **çatışma varken** uygulanıyor:
>    ölçülen saldırının tam şekli budur (kurbanın oturumu var). Pozitif kontrol
>    testte: aynı istek çapraz-site başlığı olmadan hâlâ çerezi yazıyor.
>    **Sınır:** `Sec-Fetch-Site` her tarayıcıda yok ve **yokluğu aynı-site
>    sayılıyor** (fail-open) — bu yüzden 1 ve 2 asıl korumadır, 3 ektir.
>
> **Ek olarak `Origin` kontrolü:** `POST /api/activate`'te gevşek (`Origin` varsa
> eşleşmeli; yoksa token'a güveniliyor) ve `POST /activate`'te **katı** (eşleşme
> ya da `Sec-Fetch-Site: same-origin` şart). Gerekçe: `Origin` çapraz-origin
> POST'ta her modern tarayıcıda gönderilir, yani form gönderimi için **gerçek**
> bir kontroldür; `Sec-Fetch-Site` değildir.
>
> **B2 — `base.templ` "fontlar self-host ediliyor" diyordu; edilmiyor.** Ölçüm:
> `web/static/fonts/` yok, `@font-face` sayısı **0**. Dosyanın iddiası gerçeğe
> indirildi (dış istek yok = kırmızı çizgi duruyor; marka fontu yok = sistem
> fontuna düşülüyor; self-host **M5-04**). Kart ile dosya artık aynı şeyi diyor.
>
> **B3 — kartın kendi içindeki çelişki** giderildi: §5 satır 3 kriteri artık
> yönlendirmenin **M5-04**'e ait olduğunu söylüyor.
>
> **B4 — onay kutusunu unutmak artık kullanıcının kendi davet bütçesini
> yakmıyor.** `failAttempt` bir **bütçe kapsamı** aldı: `consent_missing` yalnız
> (geniş, 60'lık) IP penceresini artırıyor; kötüye kullanım sinyalleri (yanlış
> form token'ı, tüketilmiş/süresi geçmiş kod, aktive edilemez çalışan) davet
> penceresini artırmaya devam ediyor. Ölçüldü: 30 arka arkaya onay hatasından
> sonra çalışan **hâlâ** aktive olabiliyor, ve pozitif kontrol olarak tüketilmiş
> kodu 11 kez denemek hâlâ **429** veriyor (`TestB4_*`). Eski iddia ("meşru akış
> sınıra yapısal olarak değmez") yalnız **hatasız** akış için doğruydu.
>
> **B5 — sabit pencere (fixed window) sınırı yazıldı** (`ratelimit.go`): pencere
> sınırında kısa sürede 2×limit mümkün. İddia edilmiyor, **sınır** olarak duruyor;
> kayan pencere/token bucket paylaşılan depoyla birlikte M8'de değerlendirilir.
>
> **Yeni uç nokta:** `POST /activate` (yalnız çatışma onayı). Yeni audit eylemi:
> `activation.blocked`. Yeni görünüm: `pages.Confirm` — **bu sayfa kodu gizli
> alanda taşır** ve paketin "gövdede kod yok" kuralının **bilinçli tek
> istisnasıdır**: yalnız çatışmalı çapraz-site gelişte render edilir; o durumda kod
> ya ziyaretçinindir ya saldırganın (zaten bildiği). Normal akış bu sayfayı hiç
> görmez.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı 3. tur — yeni üçüncü göz RED
> sonrası).** 2. turun B1 düzeltmesi bağımsız denetimde **doğrulandı ve
> non-vacuous kanıtlandı** (denetçi üç önlemi tek tek sabote edip testleri
> kırmızıya döndürdü, sonra dosyaları sha256 ile geri yükledi). Bu turda **iki
> yeni bloklayan** çıktı.
>
> **🔴 1. Sınırsız, SİLİNEMEZ `audit_log` yazımı.** İki dal (`overInviteBudget`
> ve `recordConflict`) her istekte bir satır yazıyor, **hiçbir pencereyi
> artırmıyordu**. Ölçüldü — önce/sonra:
>
> | Sonda | Önce | Sonra |
> |---|---|---|
> | 300 × `GET /activate?code=<tüketilmiş>` | 290×429, **300 satır** | 290×429, **60 satır** |
> | 400 × `POST /api/activate` (ölü kod) | 390×429, **400 satır** | 390×429, **60 satır** |
> | 500 × çapraz-site GET (çakışan oturum) | **0×429**, **500 satır** | **440×429**, **60 satır** |
>
> Ön koşul yalnızca **ölü bir davet linki** — yani aktive olmuş her çalışanın
> telefonunda duran kendi linki; oturum, kimlik, geçerli kod gerekmiyor. Ve
> `audit_log` **veritabanı seviyesinde** append-only (`tappa_owner` bile
> `DELETE` alamıyor), yani satırlar **kalıcı**. Bu, §4.6'nın korumaya çalıştığı
> izin kendisine karşı bir servis engelleme saldırısıydı. **Düzeltme:** her iki
> dalda `a.ipLimiter.fail(ip)`. `ratelimit.go`'nun "yalnız başarısızlıklar
> sayılır" ve "DB işinden önce" cümleleri de gerçeğe indirildi. **429'un izi
> kaybolmuyor:** WARN log satırı duruyor (IP-başına aşımın audit satırı olmaması
> §4.6 ihlali değildir — denetçiyle mutabık). Pozitif kontrol: 200 ardışık
> **başarılı** aktivasyon hiç kısıtlanmıyor.
>
> **🔴 2. M5-01'in RED'i yeni pakette yeniden üretilmişti.**
> `handler.activationState.code` **çıplak `string`**'di; ölçüldü: `%v`/`%+v`/
> `%#v` ham kodu bastı — sarmalayıcı struct'ta da. `invite.Code`'un `*string`
> dolaylılığı, değer tipinden çıkarıldığı anda düşüyordu. **Düzeltme:** alan artık
> `invite.Code`; ham dize yalnızca iki istek giriş noktasında ve `set`'in tek
> satırında **fonksiyon-yerel** olarak var. Mutasyon kanıtı: alanı `string`'e
> çevirince yeni test **7 render yolunda** kırmızı (`TestActivationState_*`).
>
> **Bloklamayan beş madde de kapatıldı:**
> 1. **`heldBy` artık DB hatasında fail-CLOSED.** Önce ölçülmüştü: `Verify` hata
>    verince `Submit` 409'u atlayıp oturum üretiyordu — koruma tam da sistem
>    hastayken kendini kapatıyordu. Artık "bilmiyorum" ≠ "oturum yok": 500 +
>    "tekrar dene", hiçbir şey tüketilmiyor. Mutasyonla kanıtlandı.
> 2. **Adres çubuğu iddiası indirildi:** çakışmalı çapraz-site varış 303 değil
>    **200** döner, yani o URL (kod dahil) geçmişte kalır. İstisna `cookies.go`'da
>    yazılı.
> 3. **`view.go` kendi içindeki çelişki giderildi:** `ConfirmView.Code` artık
>    paket dokümanında **adıyla anılan tek istisna**.
> 4. **"Bir kod harcanana kadar hiçbir kayıt yazmıyor" cümlesi** hem kodda hem
>    kartta düzeltildi (yukarıdaki kabul kriteri): yazılan `audit_log` satırı
>    §4.6 gereği **doğru**, yanlış olan cümleydi.
> 5. **Önlem 3'ün koşulluluğu ve M5-04 devri yazıldı.** Oturumsuz telefonda
>    çapraz-site GET hâlâ çerez ekiyor; bugün saldırgana **ek yetenek
>    kazandırmıyor** (aynı linki doğrudan da gönderebilirdi) ama **M5-04** `GET /t`
>    oturumsuz tap'i `/activate`'e yönlendirdiğinde bu ekili çerez o yolun girdisi
>    olur. Risk `cookies.go`'da **M5-04 adıyla anılarak** yazıldı; bugünkü azaltım
>    formun **hangi işletme + hangi çalışan** için aktive olduğunu hem üstte hem
>    **butonun hemen üstünde** söylemesi. Ayrıca `Sec-Fetch-Site` karşılaştırması
>    **büyük/küçük harf duyarsız** yapıldı — ölçülmüştü: `Cross-Site` ve
>    `CROSS-SITE` çerezi ekiyordu (M5-01'in `HTTPS://` hatasıyla aynı sınıf).
>
> **B2 iki dosyada daha düzeltildi:** `web/static/css/input.css` ve
> **`.claude/skills/tappa-brand/SKILL.md`** — skill bir **spec**'tir ve yanlış bir
> spec sonraki UI ajanını yanıltır (agent-brief md. 6). Üçü de artık aynı şeyi
> diyor: dış bağlantı yok (kırmızı çizgi duruyor), marka yazı tipleri **henüz
> yok**, sahibi **M5-04**.
>
> **Bileşen tekrarı giderildi:** `web/templates/components/` açıldı
> (`Notice` + `Card`); `border-l-4 border-tomato bg-paper px-4 py-4` zinciri
> `activate.templ`'de **5 → 1**.
>
> **Kapsam dışı bırakılan, sınır olarak yazılan:** **çerez gölgeleme** — bir alt
> alan adı aynı isimle ikinci bir `tappa_activation` yazabilir ve `r.Cookie`
> ilkini alır. Standart çare `__Host-` önekidir; `internal/session` onu
> `http://localhost` geliştirmeyi bozduğu için **bilinçli olarak** ertelemişti.
> Hangi hostların bu alan adı altında var olacağı bir **dağıtım** kararıdır → M8.
> Kod değiştirilmedi, yalnız `cookies.go`'ya yazıldı.

> **Kart düzeltmesi (2026-07-31, M5-02 B fazı 4. tur — `tappa-security-auditor`
> ONAY + son bulgular).** Güvenlik denetimi 3. turun iki bloklayanını kendi
> ölçümüyle kapalı buldu (300 GET → **tam 60** satır; 500 çapraz-site GET →
> **440×429 + tam 60** satır; `%+v` matrisinde `invite.Code` her verb'de redacted,
> pozitif kontrol ham dizenin sızdığını gösteriyor). Bu turda **bir ana bulgu ve
> beş küçük madde** kapatıldı.
>
> **⚠️ Oran sınırı BAŞARILARI da reddediyordu — bütçeler ayrıldı.** Tek bir per-IP
> penceresi her handler'ın ilk kontrolüydü, yani **geçerli** bir aktivasyonu da
> reddediyordu. Ölçüldü: **60 × bilinmeyen kod** (kimlik, oturum, geçerli kod
> gerektirmez) → ardından **geçerli** kodla gelen çalışan **429** alıyor, hâlâ
> `invited`. Ağırlaştıran: `clientIP` bilinçli olarak `X-Forwarded-For` okumuyor
> ve dağıtım **tek VPS + ters proxy**, yani proxy arkasında **tüm internet tek
> anahtarı** paylaşır — 0.1 istek/sn ile ürünün aktivasyonu süresiz kapatılabilir.
>
> **Üç bütçe, üç iş** (`ratelimit.go`):
>
> | bütçe | anahtar | neyle artar | neyi reddeder |
> |---|---|---|---|
> | `flood` (600/10dk) | IP | **her istek** | gerçek DoS — **geçerli aktivasyonu reddedebilen tek bütçe** |
> | `unknown` (60/10dk) | IP | **tenant'sız** başarısızlıklar (bilinmeyen kod, çerez yok, yabancı Origin) | hiçbir şeyi — yalnız **süreç log'unu** sınırlar |
> | `invite` (10/10dk) | davet UUID'si | **atfedilebilir** başarısızlıklar | `audit_log` satırlarını sınırlar |
>
> **Önce/sonra ölçüm** (aynı sondalar):
>
> | sonda | 3. tur | 4. tur |
> |---|---|---|
> | 300 × `GET` (ölü kod) | 290×429, 60 satır | 290×429, **11 satır** |
> | 400 × `POST` (ölü kod) | 390×429, 60 satır | 390×429, **11 satır** |
> | 500 × çapraz-site GET | 440×429, 60 satır | 490×429, **11 satır** |
> | 60 bilinmeyen → 1 **geçerli** kod | **429** (kilitli) | **303 (servis ediliyor)** |
> | 605. istek, tek adres | — | **429** (kalkan hâlâ ısırıyor) |
>
> Bloklayan #1'in kazanımı **bozulmadı, iyileşti** (60 → 11): `overInviteBudget`
> artık satırı yalnız **pencere eşiğini geçen ilk istekte** yazıyor
> (`limiter.firstOverLimit`), `recordConflict` ise davet penceresini artırıyor.
> **Bu sınırın kapsamı yazıldı:** davet **başına**dır; N farklı geçerli davet
> tutan bir çağıran N× yazabilir ve onu sınırlayan `flood` tavanıdır — yani
> satır sayısı asla istek sayısını aşmaz.
>
> **Çürütülen iddia gerçeğe indirildi** (`ratelimit.go` + yukarıdaki 5. madde):
> "meşru akış sınıra **yapı gereği** değmez" akışın **kendi katkısı** için
> doğrudur (200 ardışık başarılı aktivasyon → sıfır 429), ama "akış **her koşulda
> servis edilir**" demek **değildir** — `flood` tavanı aynı kaynak adresten gelen
> herkese uygulanır.
>
> **🔴 M5-03'e ADLANDIRILMIŞ DEVİR — kapatılmadı, kapatılamaz.** Gerçek istemci
> IP'si (`X-Forwarded-For` + `cfg.TrustedProxies`) çözülene kadar: (a) `flood`
> tavanı per-caller değil **global**'dir → dışarıdan biri onu harcayıp herkesin
> aktivasyonunu pencere boyunca engelleyebilir; (b) `unknown` bütçesinin log
> susturması da global'dir. Bütçe ayrımı bunun **ucuz** biçimini kaldırdı (altmış
> kötü link artık gerçek bir linki reddetmiyor); geri kalanı burada ayarlanabilir
> bir sayı değil, **M5-03'ün işi** — ve M5-03 `floodLimit`'i de yeniden
> değerlendirmeli (per-adres trafiğe göre seçildi). Aynı uyarı
> `activate.go` → `clientIP` ve `ratelimit.go` başlığında da yazılı.
>
> **Beş küçük madde:**
> 1. **`cookies.go` ile `view.go` çelişkisi giderildi.** `pages.ConfirmView.Code`
>    **exported düz `string`**'dir ve `%+v` onu **basar** (ölçüldü; aynı koşuda
>    `invite.Code` redacted). **Redakte edilemez:** templ değeri gizli alana
>    yazarken tipin dize biçimini ister — redakte eden tip "…(redacted)" post
>    ederdi. Bu yüzden istisna **ilan edildi**, tek görünümde, tek yolda; koruma
>    tip sistemi değil **inceleme** ve bu artık her iki dosyada da böyle yazılı.
> 2. **"Normal akış bu sayfayı hiç görmez" YANLIŞTI** (ölçüldü): **ortak dükkân
>    telefonu** — biri oturum açıkken ikinci çalışanın **kendi** linkini sohbet
>    uygulamasından açması (daima çapraz-site) tam da bu sayfayı render eder. Yani
>    "URL'de kod kalıyor" istisnası nadir değil; düzeltildi.
> 3. **Origin-red dalları artık ücretsiz değil:** `unknown` bütçesini artırıyorlar
>    ve log satırı bütçe dolunca **susuyor** (bir kez "susturuyorum" diyerek).
>    Ölçüldü: 400 istek → 60+ değil, **sınırlı** satır.
> 4. **`GET /activate/done` kalkanın arkasına alındı** — yazma yok ama istek başına
>    iki DB okuması var; `ratelimit.go`'nun "DoS kalkanı" cümlesi artık akışın
>    tamamını kapsıyor.
> 5. **§4.6 asimetrisi giderildi:** `cookies.Set` hatası artık `sessions.Issue`
>    hatası gibi `audit_log`'a yazıyor — iki dal **aynı durumu** bırakıyor (davet
>    tüketilmiş, çalışan `active`, kullanılabilir oturum yok). Pratikte erişilemez
>    bir dal; zaten bu yüzden asimetri incelemeden geçmişti.

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
- Tenant bağlamı `WithTenant` üzerinden; handler'da elle `SET` yok. ⚠️ Bu kriter
  **klasik bir tenant middleware'i ile karşılanamaz** — tap yolunda tenant
  çözümlemenin sonucudur (ADR 0002 md. 7). Karşılanma biçimi ve ölçümü aşağıdaki
  kart düzeltmesi md. 1'de.
- Oran sınırı: tap uçları için IP + oturum bazlı. Sınır, meşru bir tap'in **asla
  değemeyeceği** kadar geniş olmalı; aşan istekler `429` alır ve **tenant
  çözülmüşse** `audit_log`'a yazılır (pencere başına bir kez), çözülmemişse
  yapılandırılmış WARN log'una — sessizce düşürülmez. (§4.6 "kayıt asla
  kaybolmaz" ile çelişmemesi için: sınır bir kötüye kullanım kalkanıdır, meşru
  mesaiyi eleyen bir filtre değil. Debounce zaten guardrail'de.) ⚠️ `audit_log`'un
  neden her hâlde yazılamadığı: kart düzeltmesi md. 2.
- Panik kurtarma zaten var; 500 dönerken tap kaydı kaybolmuyor mu — M5-05'te
  ayrıca ele alınır.

**Tuzaklar.**
- `middleware.RealIP` chi'de başlığa **koşulsuz** güvenir. Tappa'da bu yeterli
  değil; `TrustedProxies` ile sınırlı kendi middleware'ini yaz veya RealIP'i
  yalnız güvenli dağıtımda kullan. Bu, denetimde R5 kapsamına girer.

> **Kart düzeltmesi (2026-07-31, M5-03 uygulaması sırasında).** Üç kriterden
> **biri** gerçekle çelişiyordu, biri **kısmen** karşılanabiliyordu; ikisi de
> zayıflatılmadan yeniden yazıldı. Gerçek IP kriteri **aynen** karşılandı.
>
> **1) "Tenant bağlamı `WithTenant` üzerinden" — ikinci yarısı doğru, ilk yarısı
> KLASİK BİR TENANT MIDDLEWARE'İ İLE KARŞILANAMAZ.** Ölçüldü, varsayılmadı:
> `grep -rn 'SELECT set_config' --include='*.go' cmd internal` üretim kodunda
> **tek** satır döndürüyor (`internal/db/tenant.go:76`, `WithTenant`'ın içi) —
> yani "handler'da elle `SET` yok" kuralı **tutuyor** ve M5-03 onu bozmadı.
>
> Ama tap yolunda tenant **isteğin girdisi değil, çözümlemenin SONUCUdur**
> (ADR 0002 md. 7): istek bir tag UID'si ve bir oturum çerezi taşır, ikisi de
> tenant taşımaz; `app.tenant_id` tam da o iki aramanın çıktısıdır. Okunacak bir
> host/path/başlık yok — uydurulsaydı **çağıran kendi tenant'ını seçebilirdi**,
> yani §4.5'in engellemek için var olduğu şey olurdu. Bu yüzden "middleware"
> kelimesini karşılamak için işe yaramaz bir katman **yazılmadı**.
>
> **Yerine kurulan mekanizma:** `httpx.Identify` — oturum çerezini istek başında
> **bir kez** çözer ve **olguları** context'e koyar (`httpx.Identity`);
> `httpx.SessionTenantID(r)` ile okunur. Karar vermez, yanıt yazmaz, `Touch`
> etmez. Gerekçe kodda (`internal/httpx/identity.go` başlığı) ve aşağıdaki 4.
> maddede.
>
> **2) "Aşan istek `429` alır ve `audit_log`'a yazılır" — ancak kimlik
> çözüldükten SONRA mümkün.** `audit_log.tenant_id` **NOT NULL** + `tenants`
> FK'si (00005); tenant'ı bilinmeyen bir olay **saklanamaz** ve "sistem tenant'ı"
> uydurulmadı (M5-02 aynı duvara çarptı, `db/queries/audit.sql` gerekçeyi yazıyor).
> Bir tap isteğinde tenant, oturum çözülene kadar bilinmez. Teslim edilen ayrım:
> - oturumu çözülmüş istek → **`tap.rate_limited` audit satırı**, pencere başına
>   **bir kez** (`FirstOverLimit`) + WARN;
> - oturumsuz istek → yalnız **WARN**, o da pencere başına bir kez.
>
> İkisi de bilinçli olarak sınırlı: `audit_log` veritabanı seviyesinde
> append-only, yani istek başına satır yazan bir 429 dalı **korunan şeye daha ucuz
> bir saldırı** olurdu (M5-02 bloklayan #1). Ölçüldü: 20 arka arkaya reddedilen
> istek → **tam 1** satır (`TestTapLimiter_BySession`); mutasyon (`FirstOverLimit`
> → `true`) testi **kırmızıya** çeviriyor.
>
> **3) Tap uçları YOK, bu yüzden middleware MONTE EDİLMEDİ.** `GET /t` ve
> `POST /api/checkin` M5-04/M5-05'in. `httpx.TapLimiter` yazıldı ve testlendi
> (`ByAddress` + `BySession`), ama hiçbir rotaya bağlanmadı — var olmayan bir
> rotanın önüne kalkan koymak, bu dosyanın tutamayacağı bir iddia olurdu. Montaj
> sırası **sözleşme olarak** `TapLimiter`'ın doküman bloğunda yazılı ve
> zorunludur:
> `ByAddress` → `Identify` → `BySession`. Sıra taşıyıcıdır: DB'ye dokunmadan yük
> atabilen tek yer `Identify`'dan **önce**dir, oturuma göre ölçebilen tek yer ise
> **sonrası**dır.
>
> **4) 🔴 N5 devrinin OTURUM YARISI teslim edildi; TAG yarısı M5-05'te.**
> `Identity.TenantID()` çözümlenen oturumun tenant'ını taşıyor ve
> `httpx.SessionTenantID(r)` ile okunuyor (`TestSessionTenantID`). **Delik
> kapanmadı ve kapandığı iddia edilmiyor:** `sys:tenant-mismatch` ancak
> `tap.Input` **iki** tenant'ı da taşıdığında ısırır ve tag çözümü istek sınırında
> yapılmaz (ADR 0002 md. 7, context-less) — o yüzden `TagTenantID` M5-05'in işi.
> M5-03'ün katkısı, M5-05'in artık besleyecek bir kaynağı olması.
>
> **5) `floodLimit` yeniden değerlendirildi (devir gereği) ve bilinçle 600'de
> BIRAKILDI.** Sayı zaten adres-başına trafiğe göre seçilmişti; değişen **sayı
> değil ANAHTAR**: `clientIP` artık `httpx.ClientIP`'i soruyor, yani tavan gerçek
> arayana ait. Düşürme reddedildi (bir NAT'ın arkasındaki tüm mekân tek adresi
> paylaşır ve bu, **geçerli** bir aktivasyonu reddedebilen tek bütçedir);
> yükseltme bir şey satın almıyor. **Ölçüldü:** proxy arkasındaki iki istemciden
> biri tavanı harcadıktan sonra öteki hâlâ **200** alıyor; aynı testin **negatif
> kontrolü** (çözücüsüz, eski durum) ötekine **429** veriyor
> (`TestClientIP_ProxiedCallersDoNotShareABucket`). **Kalan sınır yazıldı:** aynı
> NAT'ın arkasındaki düşmanca bir cihaz o mekânın tavanını harcayabilir — adres
> anahtarlı hiçbir bütçe bundan iyisini yapamaz.
>
> **Kart dışı yan etkiler (kapsam işaretleri):**
> 1. **Sabit-pencere limiter `internal/httpx`'e taşındı** (`httpx.Limiter`) —
>    CLAUDE.md §3 oran sınırını httpx'e koyuyor ve tap uçları aynı ilkeli istiyor;
>    ikinci bir kopya kaçınılmaz olarak sürüklenirdi. `internal/handler` artık
>    `type limiter = httpx.Limiter` diyor: **üç bütçe, kimin neyi artırdığı ve
>    neyi reddettiği DEĞİŞMEDİ** (M5-02'nin dar sınırı korunuyor, kart gereği
>    `/api/activate` geniş tap sınırının kapsamına girmiyor).
> 2. **`limiterMaxKeys` 20 000 → 100 000.** Gerçek istemci adresleri çözülünce
>    anahtar kardinalitesi arttı; eski tavan artık ulaşılabilir ve aşıldığında map
>    **toptan sıfırlanıyor** (fail-open, bilinçli). Bellek **ölçüldü**: 100 bin
>    canlı pencere = **7.9 MB** heap (go1.26/darwin-amd64, `runtime.MemStats`).
>    Dağıtık bir kaynağı hiçbir süreç-içi sabit pencere yenemez — o M8'in
>    paylaşılan store'u.
> 3. **`httpx.RateKey`: IPv6 `/64` ile kovalanıyor**, IPv4 birebir adresle. Tek bir
>    IPv6 host'u kendi `/64`'ünden milyarlarca adres kullanır; adres başına bütçe
>    bütçe değil, davetiyedir. (`TestRateKey_IPv6RotationSharesOneBucket`, pozitif
>    kontrolüyle.)
> 4. **Onurlandırılmayan başlıklar SİLİNİYOR.** `RealIP` çalıştıktan sonra
>    `X-Forwarded-For`, `X-Real-IP`, `True-Client-IP` istekten kaldırılıyor: chi'nin
>    `RealIP`'inin inanacağı üçlü. Amaç yapısal — bu noktadan sonra istemci adresi
>    için **tek otorite** `httpx.ClientIP`'tir ve bir handler kazara ham başlığa
>    uzanamaz.
> 5. **`tapSessionLimit` ilk taslakta 120'ydi ve kendi testi çürüttü:** 10 dakikada
>    5 saniyede bir yenilenen bir telefon **tam 120** istek üretir, yani sınır
>    "meşru akış asla değmez" kriterini karşılamıyordu. 300'e çıkarıldı (2 saniyede
>    bir istek); aritmetik artık yorum değil **test**
>    (`TestTapLimiter_DefaultsAreWideEnoughForAShiftChange`).
>
> **Açık bırakılan sınırlar (iddia değil, sınır):**
> - **Ters proxy'nin `X-Forwarded-For`'a EKLEDİĞİ (append) varsayılıyor.** Başlığı
>   **değiştiren** (replace) bir proxy, istemcinin iddiasını bizim hop'umuzun
>   imzasıyla teslim eder ve bu taraftan ayırt edilemez. Dağıtım kontrol listesi
>   maddesi (M8), kod savunması değil.
> - `Identify` **yetkilendirme kapısı değildir** ve `employees.status`'e bakmaz
>   (M5-01 devri): tap dışındaki her kimlikli yüzey durumu **kendisi** kontrol
>   etmek zorunda. Tap yolunda otorite `sys:employee-deactivated` guardrail'idir.
> - 429 yanıtı **düz metin**; markalı sayfa `tappa-brand` işidir ve yanına
>   koyulacağı tap ekranı henüz yok (M5-04 isterse değiştirir).
> - `Retry-After` pencerenin **tamamını** söyler, kalan süreyi değil (sabit pencere
>   başlangıcını dışa vurmamak için) — fazla söylemek kibar yön.
> - Oran sınırlayıcı hâlâ **süreç-içi** ve **sabit pencere** (M5-02 devri md. 2
>   aynen geçerli): iki instance sınırı ikiye katlar, pencere sınırında kısa sürede
>   2× limit mümkün. Paylaşılan store M8.

> **Kart düzeltmesi (2026-07-31, M5-03 2. tur — üçüncü göz RED sonrası).** Denetim
> 1. turun IP çözücüsünü kendi ölçümüyle **doğruladı** (26 satırlık kendi tablosu,
> canlı TCP soketi, obs-fold devam satırı, 100 000 girişli zincir, 6 mutasyon) ama
> **bir bloklayan** buldu — bu oturumun **altıncı** "yorum, sağlanmayan bir garantiyi
> beyan ediyor" vakası.
>
> **🔴 `strippedHeaders` bir DENYLIST'ti, yorum ise "yapısal" ve "hiçbir handler
> kazara ham başlığa uzanamaz" diyordu.** Ölçüldü (harici paketten, düzeltmeden
> **önce**): liste üç isim taşırken **dokuz** başlık handler'a ulaşıyordu —
> `Forwarded` (RFC 7239 **standardı**, bu dağıtımda değerini yazan hiçbir şey yok →
> tümüyle istemci kontrollü), `CF-Connecting-Ip`, `X-Client-Ip`,
> `X-Cluster-Client-Ip`, `Fastly-Client-Ip`, `X-Original-Forwarded-For`,
> `X-Forwarded`, `Forwarded-For`, `X-Forwarded-Host`. Bugün üretimde bunları okuyan
> kod **yok** (kendi ölçümüm: üretim Go'sunda okunan istek başlıkları
> `Sec-Fetch-Site`, `Origin` ve `r.UserAgent()`), yani **canlı açık değildi**; ama
> iddia yanlıştı ve M5-04 bir satırla doğru yapabilirdi.
> **Düzeltme iki parçalı:** (a) liste 13 isme genişletildi (Azure/App Engine dâhil);
> (b) **iddia kapsamına indirildi** — bu bir denylist'tir, bilinmeyen bir satıcı
> başlığı hayatta kalır; doğru cümle "istemci adresi `ClientIP`'ten gelir, adres
> için **herhangi** bir başlık okumak yeni bir hatadır". Ölçüm: SURVIVED **9 → 0**;
> iki mutasyon (silmeyi kaldır / listeyi eski üç isme daralt) testi **kırmızıya**
> çeviriyor, ve **pozitif kontrol** listenin "her başlığı sil" olmadığını gösteriyor
> (`X-Forwarded-Host`, `X-Forwarded-Proto`, `Origin`, `Cookie` **geçiyor** —
> `X-Forwarded-Host` bilinçli kapsam dışı: adres değil host taşır, URL kuran kodun
> ve M8'in işi).
>
> **Bloklamayan dört madde de kapatıldı:**
> 1. **Tap kalıntısı ADIYLA yazıldı.** 3000/10dk **paylaşılan** adres bütçesini
>    düşmanca bir cihaz harcarsa o mekânın **meşru tap'leri 429 alır** ve 429 dalı
>    **ne `transactions` ne `flag`** üretir — istek karar motoruna hiç ulaşmaz.
>    §4.6 ile gerçek bir gerilim; **çözülmedi, sınırlandı** ve M5-04/M5-05 monte
>    etmeden **önce** görsün diye `TapLimiter` doküman bloğunun başına kondu.
>    Kartın *"meşru bir tap'in asla değemeyeceği"* cümlesi M5-02'nin ayrımıyla
>    düzeltildi: akışın **kendi katkısı** için doğru, **servis edilmesi** için
>    değil. Aktivasyonda aynı kalıntı var ama bedeli farklı — reddedilen aktivasyon
>    10 dk sonra tekrarlanır, reddedilen check-in bordroda **eksik saattir**.
> 2. **`BySession` sözleşmesi artık ZORLANIYOR.** Denetçi ölçtü: `Identify`
>    unutulunca sıfır `Identity` → `Session.ID == uuid.Nil` → **100/100 istek
>    ölçülmeden** geçiyordu; dosya sırayı "the contract" diye adlandırıyordu ama
>    hiçbir şey kontrol etmiyordu. Artık `SessionUnresolved` bir **wiring hatası**
>    olarak ele alınıyor: **500 + `slog.Error`**, sessiz geçiş yok. ⚠️
>    **`SessionAbsent` bununla karıştırılmadı** — oturumsuz tap meşrudur (§5 satır 3)
>    ve **geçiyor**; ayrımı yapan zaten `SessionState`'in sıfır-değer kutbu, ve bu
>    onun ilk gerçek tüketicisi. İki mutasyon (kontrolü kaldır / `SessionAbsent`'i de
>    reddet) testi kırmızıya çeviriyor.
> 3. **Varsayılan rota (`0.0.0.0/0`, `::/0`) artık bir kapıdan geçiyor —
>    KAPSAM GENİŞLEMESİ, gerekçeli.** Denetçi ölçtü: zincirin tamamı güvenilirse
>    yürüyüş duracak **güvenilmeyen** adres bulamaz ve **en soldaki** (istemcinin
>    yazdığı) değeri döndürür — yani "herkese güven" trust sınırını genişletmez,
>    savunmayı **tersine çevirir**. `internal/config`: prod'da **başlangıç hatası**,
>    aksi hâlde **gürültülü WARN**. Gerekçe paketin kendi kuralı ("güvenlik niteliği
>    olan bir değer sessiz varsayılana düşmez"). **Mevcut değerlerin tamamı tarandı**
>    ve hiçbiri kırılmıyor: `.env` = `127.0.0.1/32` (dev), `.env.example` =
>    `127.0.0.1/32`, CI **hiç set etmiyor** (→ boş), compose set etmiyor,
>    `config_test` boş kullanıyor. **Sınır yazıldı:** kapı yalnız `/0` yakalar; `/1`
>    ya da gerçek ingress'ten çok geniş bir aralık hâlâ kabul edilir — "ne kadar
>    geniş fazla geniş" bu paketin bilemeyeceği bir dağıtım gerçeğidir. Davranış
>    `realip.go`'da da yazılı ve **teste bağlandı**
>    (`TestResolveClientIP_DefaultRouteReturnsTheClientsOwnClaim` + `::/0`'ın IPv4
>    zincirini **etkilemediğini** gösteren pozitif kontrol).
> 4. **Terminoloji:** tavan aşılınca dönen değer "innermost" değil, yürünenlerin en
>    **dıştaki** (outermost) trusted hop'u. Düzeltildi — ve aynı yerde **fazla güçlü
>    bir cümle** de indirildi: "hiçbir meşru istek oraya çözülmez" **yanlıştı**
>    (zincirsiz gelen bir proxy health-check'i tam oraya çözülür); doğrusu "hiçbir
>    **tap** oraya çözülmez".
>
> **Mutlak iddia taraması (son tur).** Bu turda değişen dosyalardaki
> *cannot/never/only/structural* kalıpları tek tek tarandı; ölçülemeyen üç cümle
> daha indirildi: (a) `SessionTenantID`'nin "çağıran uuid.Nil'i tenant sanamaz"ı →
> *"ayırt etme yolu vardır; `ok`'u yok saymak bu fonksiyonun engelleyemeyeceği bir
> tercihtir"*; (b) `BySession`'ın "tenant adlandırabilen tek bütçe"si → *"bu montaj
> sırası altında tek"* (`refuse` kimliği her iki dalda da kontrol ediyor);
> (c) hop-cap yorumundaki "yalnız bizim aralığımızı sahteleyen bir çağıran
> ödeyebilir" → *"ya elli gerçek proxy ya da sahteleyen bir çağıran"*.

> **Kart düzeltmesi (2026-07-31, M5-03 3. tur — `tappa-security-auditor` RED
> sonrası).** Bulgu **2. turda eklediğim kapının içindeydi** ve yine aynı sınıf
> (yedincisi).
>
> **🔴 Varsayılan rota kapısı EŞDEĞER BİR YAZIMLA atlatılıyordu.**
> `trustedProxySanity` **ham** prefix'te `Bits()==0` bakıyordu; `httpx` ise bir
> **4-in-6** prefix'i unmap edip 96 bit çıkarıyordu. Yani `::ffff:0.0.0.0/96`
> kapıya **96** görünüyor, çözücüde **`0.0.0.0/0`** oluyordu. Gerçek `config.Load`
> + gerçek `httpx.NewRouter`, `TAPPA_ENV=prod`, peer **sıradan bir internet
> çağıranı** (`198.51.100.7`), `XFF: 192.0.2.55, 1.1.1.1` — **kendi ölçümüm,
> denetçininkiyle birebir**:
>
> | `TAPPA_TRUSTED_PROXIES` | ÖNCE | SONRA |
> |---|---|---|
> | `0.0.0.0/0` | REFUSED | REFUSED |
> | `::/0` | REFUSED | REFUSED |
> | `::ffff:0.0.0.0/96` | **yüklendi, client=192.0.2.55** 🔴 | REFUSED |
> | `::ffff:10.0.0.1/96` | **yüklendi, client=192.0.2.55** 🔴 | REFUSED |
> | `127.0.0.1/32,::ffff:0.0.0.0/96` | **yüklendi, client=192.0.2.55** 🔴 | REFUSED |
> | `::ffff:0.0.0.0/104` | yüklendi, client=198.51.100.7 | REFUSED |
>
> Prod'da ne hata ne uyarı vardı; tek satırlık bu yazım **§5 satır 6'nın 50 güven
> puanını** uydurulabilir kılıyordu.
>
> **Seçilen tasarım ve gerekçesi (koordinatör iki seçenek verdi; üçüncüsü
> seçildi).** "Kontrolü normalize et" iki pakette **iki kopya** kanonikleştirme
> demekti — hatanın kaynağı tam olarak buydu. "Normalizasyonu config'e taşı" tek
> nokta verirdi ama `httpx` zaten `config`'i import ediyor, yani `config → httpx`
> **döngü** olurdu; döngüyü kırmak için en alt katmanı HTTP router'ına bağlamak
> gerekirdi. Bu yüzden **ikinci temsil tümden silindi**: `config.prefixes` 4-in-6
> yazımını **her ortamda reddediyor** (mesaj IPv4 karşılığını yazıyor),
> `httpx.acceptPrefixes` ise unmap etmek yerine **düşürüyor** — böylece `RealIP`'i
> doğrudan kuran bir çağıran da o forma ulaşamıyor. Artık bir aralığın **tek**
> yazımı var: kapı ile çözücünün farklı şeye bakması **mümkün değil, çünkü
> bakılacak tek şey var**. Bedeli yazıldı: `::ffff:10.0.0.0/104` yazan operatör
> artık başlangıçta hata alır (sessizce herkese güvenmekten de, sessizce ölü bir
> girdiden de iyidir). İki yarı **bağımsız olarak taşıyıcı**: her birini ayrı ayrı
> geri alan mutasyon (M15, M16) uçtan uca testi **kırmızıya** çeviriyor.
> **İki kural ayrı:** varsayılan rota bir **risk yargısıdır** (prod hata /
> prod-dışı WARN); 4-in-6 bir **belirsiz yazımdır** ve **her ortamda** reddedilir.
> `Bits()==0` kontrolü artık "kendi başına" değil, **bu reddin sayesinde**
> tamdır ve dosya bunu böyle yazıyor. Test boşluğu kapandı: `::ffff:0.0.0.0/96`,
> `::ffff:10.0.0.1/96`, `1.2.3.4/0`, karışık liste satırları + uçtan uca
> (`config.Load`+`NewRouter`) regresyon + gerçek ingress listesinin hâlâ
> çalıştığını gösteren **pozitif kontrol**.
>
> **Genel ders (üçüncü kez):** *bir değeri doğrulayan kontrol, o değeri kullanan
> kodun gördüğü biçimi görmelidir.* M5-01'de `HTTPS://`, M5-02'de `Cross-Site`,
> burada `::ffff:`. Ders `config.go`'ya yazıldı.
>
> **Bloklamayan üç madde:**
> 1. **`router.go`'daki indirilmemiş ikiz düzeltildi.** `realip.go`'daki cümleyi
>    2. turda kapsamına indirmiştim ama `router.go` düz düz *"httpx.ClientIP is the
>    single authority"* diyordu — repo iki şey söylüyordu. Artık kural olarak
>    yazılı ve `realip.go`'ya işaret ediyor.
> 2. **Denylist 13 → 32 isim.** Denetçi **canlı TCP soketinden** ölçtü: 36 adayın
>    **23'ü** hâlâ handler'a ulaşıyordu — `Client-IP`, `Proxy-Client-IP`,
>    `WL-Proxy-Client-IP` gibi **klasik listelerin tamamında olan** isimler dâhil,
>    yani cümle kendi ölçütünü karşılamıyordu. Ölçüm önce/sonra: **23 → 4**, kalan
>    dördü **bilinçli** (`Via` aracıları adlandırır, `X-Forwarded-Host`/`-Proto`
>    host ve şema taşır, `Origin` **CSRF kontrolünün girdisidir** — silinseydi
>    aktivasyon akışı kırılırdı). Bu dördü aynı zamanda **pozitif kontrol**:
>    "her başlığı sil" mutasyonu (M18) testi kırmızıya çeviriyor. Test artık
>    elle kurulmuş `http.Header` yerine **gerçek sokete** yazıyor.
> 3. **`writeTooManyRequests`'in doc bloğu** araya boş satır girmediği için
>    `writeUnavailable`'a yapışmıştı (godoc 429 gerekçesini 500 yardımcısına
>    atıyordu); ayrıldı.
>
> **Denetçinin doğrulayamadığı nokta (dürüstlük payı, kapatıldı):** `.env` okuma
> izni olmadığı için "`.env` = `127.0.0.1/32`" iddiasını teyit edememiş. Değer
> bu turda **yeniden** okundu (yalnız bu değişken yazdırılarak, sır basılmadan):
> `TAPPA_TRUSTED_PROXIES=127.0.0.1/32`, `TAPPA_ENV=dev` — yani yeni kapı mevcut
> geliştirme ortamını **kırmıyor**, ki canlı sunucu koşusu da bunu gösteriyor.

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

> **Kart düzeltmesi (2026-07-31, M5-04 uygulaması sırasında).** Hiçbir kriter
> düşürülmedi; ikisi gerçekle çelişiyordu ve kartın anmadığı yedi karar aşağıda
> gerekçesiyle sabitleniyor.
>
> **1) 🔴 "Sayfa açılırken sayaç ilerletilmez" MEVCUT API İLE İMKÂNSIZDI.**
> `sun.Verify` (verify.go) sabit akışının **6. adımı** `AdvanceCounter`'dır, yani
> sayfa yolunda `Verify` çağrılamaz; kripto kontrolünü ilerletmeden yapan
> `verifySUN` ise **unexported**'tı. Kart bunu bilmiyordu. Yeni giriş noktası:
>
> ```go
> func (v *Verifier) PreviewWithoutReplayProtection(ctx, p Params) (Preview, error)
> ```
>
> **Adın ve tipin işi caydırmaktır** — `Check`/`Validate`/`IsValid` bilinçli
> olarak yok. Yanlış kullanımı zorlaştıran **dört ölçülmüş** önlem:
> (a) ad "replay koruması yok" diyor; (b) dönüş tipi `Result` **değil** `Preview`
> ve ona atanabilir değil, yani `Verify` için yazılmış kod bu fonksiyonla
> **derlenmez** (`TestPreview_HasNoSUNValidField` ikisini de ölçüyor);
> (c) `Preview`'da **`SUNValid` alanı YOK** — çağıranın "SUN gerçek" sonucunu
> okuyacağı alan mevcut değil, yalnız `CMACValid` var; (d) `inspect` (ortak
> 2–5. adımlar) **unexported**, yani paketten tek çıkış ya `Verify` ya bu.
> Doküman bloğu POST'un `AdvanceCounter`'ı çağırmak **ZORUNDA** olduğunu
> büyük harflerle yazıyor; `tapcontext.go` aynı sözleşmeyi öteki uçtan yazıyor.
>
> **Kanıt (ölçüldü, iddia değil):** sahte bir resolver'ın `WithTenant`'ı
> callback'i **gerçekten çalıştırıyor** ve içindeki `pgx.Tx` stub'ı ifadeleri
> sayıyor → preview'da **0 ifade**, `Verify`'da **1 ifade** (pozitif kontrol,
> `TestVerify_StillIssuesTheAdvanceQuery` — inspect refactor'ünün regresyon
> koruması da bu). Gerçek Postgres'e karşı: 20 sayfa açılışı → `last_ctr`
> **değişmedi**, ardından `Verify` → **değişti**; sonra aynı URL preview'dan yine
> geçiyor (replay'i ayırt edemediğinin kanıtı). Canlı sunucuda 13 + 20 sayfa
> açılışı sonrası `last_ctr` 0 → **0**. `internal/sun` kapsamı **%96.7**
> (preview.go %100), §8 hedefinin üstünde.
>
> **2) İmzalı bağlam CMAC TAŞIMIYOR — kartın tuzağından bir adım öteye gidildi.**
> Tuzak "ctr/cmac'i DOM'a gömme" diyor; buradaki tasarım **CMAC'i** hiç
> gömmüyor: `<payload>.<mac>`, payload =
> `1|uid|ctr|channel|cmacVerified|tagTenant|location|issuedAt`.
> ⚠️ **Düzeltme (3. tur):** "tag, ctr ve cmac HTML'de yok" cümlesi **yanlıştı** —
> uid ve sayaç imzalı payload'ın **içinde** ve script tarafından çözülebilir;
> yalnız **CMAC** gerçekten yok (denetçi her baytı taradı). İkisi de sır değil
> (çip ikisini de aynı sayfanın adres çubuğunda basıyor), ama iddia ölçümden
> genişti. İmzanın satın aldığı şey **gizlilik değil bütünlük**: POST yalnız bu
> sunucunun, bu oturum için, yakın zamanda ürettiği bir bağlamla çalışır.
> Çipin MAC'i sunucuda **bir kez** kontrol edilir ve sayfaya yalnız **tek bitlik
> sonucu** geçer, yani §4.7 listesindeki hiçbir değer (oturum token'ı, CMAC,
> anahtar, davet kodu, GPS) bağlamda **yok** (`TestTapContext_MintedPayloadHasNoHexMAC`
> + `TestTapPage_BodyCarriesNoCryptographicMaterial` her baytı tarıyor).
> **Bedeli açıkça M5-05'e yazıldı:** `SUN geçerli == CMACVerified && atomik
> sayaç ilerlemesi` — iki yarı, VE'lenmiş. Alanı okumadan ilerleten bir POST
> sahte tap kaydeder; ilerletmeden alana bakan bir POST replay kaydeder.
> **Oturum id'si MAC girdisinde, payload'da DEĞİL** (authenticated, disclosed
> değil): bağlam yalnız servis edildiği tarayıcıda çözülür ve id sayfaya
> düşmez. ⚠️ Bu **URL paylaşmayı** (buddy punching, ADR 0005) **çözmez**.
>
> **3) Anahtar TÜRETİLDİ, dördüncü env var eklenmedi.**
> `HMAC(TAPPA_SESSION_HMAC_KEY, "tappa/tap-context/v1/key-derivation")`, üstüne
> MAC etiketi `tappa/tap-context/v1|` (alan ayrımı — üçüncü tür, kendi etiketi).
> Gerekçe: bu değer **saklanmıyor**, 15 dakika yaşıyor; zorunlu yeni bir env var
> her dağıtımı ve CI'yı, yalnız bunu imzalayan bir anahtar konana kadar
> **başlatılamaz** yapardı. HMAC tek yönlü → bu anahtarın sızması session
> anahtarını vermez. **Sınır açıkça yazıldı:** ters yön geçerli — session
> anahtarını tutan bunu türetebilir, yani ikisi **bağımsız değil**. Ölçüldü
> (`TestTapContext_DomainSeparation`): türetilen anahtar ≠ session anahtarı;
> session anahtarı altındaki çıplak HMAC bu imzayı **üretmiyor**; etiket
> çıkarılınca MAC **değişiyor**; farklı dağıtım anahtarı farklı imza veriyor.
> **TTL 15 dk** — M5-10 penceresinin (1–15 dk) **üst sınırına** eşit seçildi ki
> M5-10 indiğinde ikisinden **gevşek olan** asla bu olmasın. `IssuedAt` sunucu
> saati ve authenticated.
>
> > ⚠️ **Düzeltme (2026-08-02, M5-10 uygulandıktan sonra).** Bu maddenin son iki
> > cümlesi M5-10'dan **önce** yazılmıştı; ikisi de artık geçerli değil.
> > (1) *"M5-10 imzalı damgayı değil DB'deki en erken satırı okumalı"* — **öyle
> > olmadı:** `tap_page_views` tablosu **hiç yapılmadı** (kullanıcı kararı) ve
> > `sys:tap-freshness` sayfa yaşını doğrudan bu imzalı `IssuedAt`'ten ölçüyor.
> > Yapmama gerekçesi M5-10 kartının düzeltme bloğu **md. 3**'tedir ve bir
> > **argümandır, ölçüm değil**. (2) *"tazelik guardrail'i kurulmadı"* —
> > **kuruldu:** `TAPPA_FRESHNESS_SECONDS` (varsayılan **180 sn**) `checkin.New`
> > üzerinden `params.FreshnessWindow`'a bağlandı. `tapContextTTL` **15 dk
> > kaldı**, yani yukarıdaki *"gevşek olan asla TTL olmasın"* gerekçesi tuttu
> > (180 < 900) — ⚠️ tek istisna `TAPPA_FRESHNESS_SECONDS=900`: kabul edilen ama
> > kayıtlı bandı boşaltan **sessiz no-op**, M5-10 kartı md. **7b**.
>
> **4) 🔴 ÖN-DOĞRULAMA BAŞARISIZ OLSA BİLE SAYFA RENDER EDİLİYOR** (retired/lost
> plaket = §5 satır 1, geçersiz CMAC = §5 satır 2). İlk bakışta yanlış görünür;
> **§4.6'nın izin verdiği tek davranış budur:** üçü de `reject`'tir ve
> `reject` **kayıt gerektirir**, kaydı POST yazar. Render etmeyi reddeden bir
> sayfa butonun hiç basılmamasına, yani reddedilen denemenin **iz bırakmamasına**
> yol açardı. Tek istisna **bilinmeyen UID**: tenant'ı ve lokasyonu yok, yani
> kayıt yazılacak bir bağlam yok (404; `sun.ErrUnknownTag`'in kendi gerekçesiyle
> aynı). Aynı sebeple **çapraz-tenant plaket de render ediliyor** (venue adı
> **olmadan**) — reddetmek `sys:tenant-mismatch`'i **kayıtsız** karara bağlamak
> olurdu; karar M5-05'in.
>
> **5) §5 satır 3 BAĞLANDI ve devralınan tuzak açıkça ele alındı.** Oturumsuz
> **ve** iptal edilmiş oturum → `303 /activate`, **kayıt yok** (gerçek Postgres:
> 40 `GET /t`, yarısı oturumsuz → `transactions` **5133 → 5133**). `Identity`
> sıfır değeri tuzağı (devir md.3) mekanik olarak ele alındı: `SessionUnresolved`
> → **500 + ERROR log**, `SessionAbsent` → aktivasyon; ikisi ayrı `case`'ler ve
> ayrımı kaldıran mutasyon testleri **kırmızıya** çeviriyor.
> ⚠️ **İptal edilmiş oturum ile DEAKTİF ÇALIŞAN karıştırılmadı:** deaktivasyon
> oturum iptal etmez (M5-01), yani deaktif çalışan **canlı oturumla** sayfayı
> görür, butona basar ve `sys:employee-deactivated` denemeyi **kaydeder** (§5
> satır 4). Testte ikisi ayrı vaka.
> **Ekili aktivasyon çerezi etkileşimi (devir md.4) — DEĞERLENDİRİLDİ,
> kötüleştirilmedi:** yönlendirme saldırgana **yeni yetenek vermiyor** (çerezi
> ekmek zaten kurbanın saldırganın linkini açmasını gerektirir ve o gezinme
> **zaten** 303 ile `/activate`'e düşüp formu gösterir), değişen **ne zaman**
> görülebileceği. Azaltım M5-02'nin gönderdiği hâliyle duruyor: form hangi
> işletme + hangi çalışan olduğunu butonun üstünde yazıyor. Yönlendirmeye
> tek-kullanımlık bir işaret ekleyip `/activate`'in ekili çerezi reddetmesini
> sağlamak **bilinçli olarak yapılmadı**: o akışın çerez mantığı dört denetim
> turu sürdü ve dışarıdan yapılan bir değişiklik ölçülmüş savunmayı ölçülmemiş
> hâle getirir. `tap.go` → `redirectToActivation` bunu yazıyor.
>
> **6) `TapLimiter` MONTE EDİLDİ, sözleşme sırasıyla** —
> `ByAddress → Identify → BySession` (`handler.Tap.Mount`). Sırayı bozan mutasyon
> testleri kırmızıya çeviriyor (`BySession` `SessionUnresolved` görüp 500 veriyor).
> **429 gövdesi artık markalı** (devir md.6): `httpx.TapLimitParams.Refused
> http.HandlerFunc` eklendi — `internal/httpx` şablon **import etmiyor**,
> renderer enjekte ediliyor; `nil` eski düz metin davranışını koruyor.
> `Retry-After`/`no-store`/`nosniff` renderer'dan **önce** yazılıyor, yani
> gövdeyi değiştiren çağıran onları düşüremez. ⚠️ **429 kalıntısı (devir md.2)
> ÇÖZÜLMEDİ ve çözüldüğü iddia edilmiyor:** paylaşılan adres bütçesi tükenirse
> meşru tap 429 alır, karar motoruna ulaşmaz, ne `transactions` ne `flag` olur.
> M8 (paylaşılan store + mekân-başına anahtar).
>
> **7) Fontlar SELF-HOST EDİLİYOR — kriter karşılandı.** `web/static/fonts/`:
> Space Grotesk (variable 300–700) + IBM Plex Mono (400/700), her biri `latin`
> **ve** `latin-ext` alt kümesiyle — `latin-ext` Maltaca **ċ ġ ħ ż** içindir
> (arayüz İngilizce ama **isimler** değil). Altı woff2 toplam **79.032 bayt**
> (~77 KiB), ikiliye gömülü, `/static/fonts/`'tan servis ediliyor (canlı ölçüm:
> 200, `font/woff2`). ⚠️ **Düzeltme (3. tur):** bu satır "92 KB" diyordu; 92.126
> bayt **dizinin tamamıdır** (iki OFL metni + README dâhil), tarayıcının
> indirdiği yalnızca yazı tipleridir.
> Kaynak Google Fonts CSS API'sinin kendi woff2 dosyaları; **derleme zamanında
> bir kez** indirildi ve commit edildi — **runtime'da hiçbir dış bağlantı yok**
> (render edilen sayfada `href`/`src` yalnız `/static/css/app.css` ve
> `/static/js/tap.js`; mutlak URL sayısı **0**). Her iki aile **SIL OFL**;
> lisans metinleri, kaynak URL'ler ve sha256'lar
> `web/static/fonts/README.md`'de. `app.css`'te `@font-face` sayısı **0 → 6**.
> `input.css`, `layout/base.templ` ve **skill `tappa-brand`** aynı anda
> güncellendi (yanlış bir spec sonraki UI ajanını yanıltır).
>
> **Kart dışı yan etkiler (kapsam işaretleri):**
> 1. **Yeni paket `internal/domain/tenant`** (`Directory.TapPage`) — CLAUDE.md §3
>    "handler içinde iş kuralı veya SQL YOK" gereği; iki store çağrısı handler'a
>    konsaydı kural delinirdi. Dizin haritası bu paketi zaten adlandırıyor.
>    Venue **oturumun tenant'ı altında** okunuyor: yabancı plaket satır
>    döndürmez → başka tenant'ın venue adı bu çalışanın ekranına **düşmez**.
>    Bu bir **ifşa** tercihidir, izolasyon kararı değil (`ErrForeignLocation`
>    dosyada bunu yazıyor).
> 2. **Yeni sorgu yazılmadı;** `GetEmployeeActivationContext` ve `GetLocationWiFi`
>    yeniden kullanıldı ve **ikisinin de doküman bloğuna yeni çağıran eklendi**
>    (bir sorgu, kendi çağıranlarını sayan bir yorum taşıyorsa o yorum gerçeği
>    söylemek zorundadır).
> 3. **`layout.Page` bölündü:** ortak `shell` + `Page` (script yok) +
>    `PageWithScript` (tek self-host script). Çağıran imzaları değişmedi.
> 4. **`web/static/js/` ilk sakini** (`tap.js`). GPS **yalnız butona basınca**
>    tek `getCurrentPosition`, `maximumAge: 0` (önbellek konumu bilerek
>    reddediliyor), 8 sn timeout, her başarısızlıkta **koordinatsız gönderim**.
>    JS kapalıysa form yine çalışır (boş koordinat alanları = §5 satır 6/7'nin
>    zaten meşru saydığı durum). Hata nesnesi **loglanmıyor** (içindeki tek şey
>    konum olabilir).
> 5. **🔴 `redline-check.sh` R2 ile etkileşim — desen GEVŞETİLMEDİ.** R2 yasak üç
>    API adını **tüm repoda** tarar ve yorumları da görür; ilk taslakta kendi
>    açıklama metinlerim ve bir testim CI'yı **kırmızıya** çeviriyordu. Çözüm
>    **muafiyet eklemek değil**, kaynakta o adları **hiç yazmamak** oldu (prose
>    dahil) — ağı tartışılır hâle getirmek onu er geç gevşetir (M0-07 dersi).
>    Sayfa tarafındaki "abone olma yok" iddiası testte değil **R2'de** duruyor;
>    o daha geniş. **Ölçümün kapsamı (düzeltildi — ilk yazım fazla genişti):**
>    üç ad, **R2'nin taradığı yollarda** (`cmd internal db web/templates
>    web/static/js scripts`, `redline-check.sh` hariç) **0 eşleşme**. Repo
>    genelinde 0 **değil** ve olmamalı: `CLAUDE.md` §4.2, `docs/adr/0005`,
>    `docs/plan/*` ve `.claude/agents/tappa-security-auditor.md` bu adları
>    **meşru olarak** — yasağı tarif etmek için — içeriyor. Kural kaynak koda
>    dairdir, dokümana değil.
>
> **Açıkta bırakılan sınırlar (iddia değil, sınır):**
> - **HTTP yolunda GEÇERLİ CMAC'li uçtan uca test YOK.** Geçerli bir SDM MAC'i
>   `internal/sun` dışında üretmek, SV2+CMAC türetmesinin **ikinci bir kopyasını**
>   yazmak demekti (M2-04 byte-reversal dersi tam olarak bunun bedeli).
>   `internal/handler`'ın DB testi bu yüzden **geçersiz** CMAC kullanıyor ve
>   pozitif kontrolünü `sun.AdvanceCounter`'ı doğrudan çağırarak kuruyor;
>   geçerli-CMAC hâli türetmenin yaşadığı yerde, aynı gerçek Postgres'e karşı
>   ve kendi pozitif kontrolüyle ölçülüyor
>   (`internal/sun` → `TestPreview_AgainstPostgresDoesNotMoveTheCounter`).
> - **Seed'in plaketleri NFC yolunda 500 veriyor** (ölçüldü, canlı):
>   `test/fixtures/seed.sql` `aes_key_ref`'e KEK-sarmalı değil düz bir yer
>   tutucu yazıyor (`FAKE-WRAPPED-KEY-DO-NOT-USE-…`), Unwrap onu reddediyor.
>   QR biçimi (yalnız `tag=`) kriptoya hiç uğramadığı için **200** ve sayfa
>   render ediliyor. Bu M5-04'ün değil **seed'in** boşluğu (state.md
>   "aes_key_ref KEK-sarmalı doğrulaması" notu) — M5-09/M8'e ait.
> - **Buton yönsüz ("Tap").** Yön §5'e göre kişinin **son açık girişine** göre
>   hesaplanır, karar motorunun işidir ve sayfada tahmin edilirse bir saniye
>   sonraki onay ekranıyla çelişebilir. Yön-farkında etiket (`Tap in`/`Tap out`)
>   §9 gereği **kullanıcıya sorulmak üzere** raporlandı; kendiliğinden eklenmedi.
> - Bağlamın TTL'i **sabit** (15 dk), tenant ayarı değil. Ayarlanabilir pencere
>   M5-10'un guardrail'i.

> **Kart düzeltmesi (2026-07-31, M5-04 2. tur — üçüncü göz RED sonrası).**
> Denetim 1. turun §4.4 çekirdeğini kendi ölçümüyle **doğruladı**
> (`log_statement=all` altında `AdvanceTagCounter` için **tam 1 execute**; dört
> caydırıcının dördü de geçici bir paketten **derleme hatası** verdi; imzalı
> bağlamı openssl ile bağımsız yeniden kurup birebir eşleştirdi) ama **bir
> bloklayan** buldu — ve o bloklayan, bu oturumun kendi ders listesindeki
> kalıbın **tekrarıydı**.
>
> **🔴 M5-03'ün ONAYLANMIŞ kriteri benim montajımla üretimde ÖLÜYDÜ.**
> `handler.NewTap` `TapLimiter`'ı **`Audit` alanı olmadan** kuruyordu — tek
> üretim çağrısı. Sonuç: `httpx/ratelimit.go`'nun montaj sözleşmesi
> (`NewTapLimiter(TapLimitParams{Audit: rec, Log: log})`) ve *"oturumu çözülmüş
> bir red, pencere başına bir `audit_log` satırı bırakır"* cümlesi **middleware
> için doğru, ürün için yanlıştı**. Denetçinin canlı ölçümü: 300 istek →
> 285×200 / 15×429, red **kimlikli** (`scope=session`, `employee_id=…0301`),
> `audit_log` **4145 → 4145**. `cmd/tappa/main.go`'da `trail` **zaten vardı**;
> `NewTap`'in imzasında onu alacak parametre yoktu — unutulmuş satır değil,
> **eksik tasarım**. Ağırlaştıran: `POST /api/checkin` aynı gruba gireceği için
> boşluk M5-05'e sessizce miras kalacaktı.
>
> **Seçilen yol: BAĞLA** (koordinatör "gerekçeyle bırakmak da kabul" dedi;
> tercih edilmedi). Onaylanmış bir kriteri, yalnız montajı bana devredildiği
> için düşürmek §4.6'nın izini zayıflatırdı — yetenek M5-03'te teslim edilmişti,
> eksik olan tek şey bir parametreydi. `NewTap(preview, dir, sess, **rec**, cfg,
> log)`; nil recorder artık **başlangıç hatası** (`TapLimiter` nil'i kabul
> etmeye devam ediyor — DB'siz kurulabilmesi gerek — ama üretim yolu etmiyor).
> **Ölçüm, gerçek Postgres:** `tap.rate_limited` satırları **0 → 1** (301 istek,
> 298 servis + 3 red), `transactions` **sabit**; oturumsuz redde **0 satır**
> (audit_log.tenant_id NOT NULL — sınır, boşluk değil).
>
> **⚠️ MUTASYON İLK DENEMEDE YEŞİL KALDI — kendi testim aynı hatayı yapıyordu.**
> `Audit: rec`'i silen mutasyon testleri **kırmızıya çevirmedi**, çünkü iki
> refusal testim `tp.limiter`'ı **kendi kurdukları** (ve `Audit`'i açıkça
> geçen) bir limiter'la değiştiriyordu: yapılandırdığı şeyi denetleyen bir test
> hiçbir şey denetlemez — denetçinin bulduğu boşluğun **birebir aynısı**, bir
> katman yukarıda. Düzeltildi: testler artık `NewTap`'in kurduğu limiter'ı
> **üretim bütçesiyle** sürüyor (`httpx.TapSessionLimit()+3` istek; fake'lerle
> 0.03 sn, gerçek DB ile 1.4 sn). Yeniden koşuldu: mutasyon **iki testi de**
> kırmızıya çeviriyor. `TapSessionLimit()`/`TapAddressLimit()` bu yüzden export
> edildi ve gerekçesi `ratelimit.go`'da yazılı ("test için export" normalde
> kokar; alternatifi, ölçtüğünü sanan bir test).
>
> **Bloklamayan dört madde de kapatıldı:**
> 1. **`Preview` artık `db.ResolvedTag` TAŞIMIYOR.** Denetçi haklıydı: satır
>    `LastCtr`'ı da `AESKeyRef`'i de handler'a taşıyordu, yani
>    `pv.CMACValid && p.Ctr > pv.Tag.LastCtr` **yazılabilir bir cümleydi** —
>    §4.4'ün adıyla yasakladığı Go-tarafı TOCTOU'nun tam şekli — ve
>    *"tek sunduğu alan CMACValid"* cümlesi **harfiyen yanlıştı**. Ayrıca
>    KEK-sarmalı anahtarı bir HTTP handler'ına vermenin hiçbir sebebi yoktu
>    (§4.7). Yeni yüzey **dört alan**: `CMACValid`, `TenantID`, `TagStatus`,
>    `Location`. Reflection testi artık üçünü birden zorluyor: `SUNValid` yok,
>    **sayaç benzeri alan yok**, **ham byte alanı yok**, ve alan sayısı sabit
>    (yeni alan eklemek testi kırar — "argüman gerektirir, miras alınmaz").
>    `Verify` satırı döndürmeye devam ediyor: karar katmanı tag'in sahibi, sayfa
>    değil.
> 2. **Çift basış artık koordinatsız natif submit üretmiyor.** `tap.js`'te
>    yeniden-giriş koruması `preventDefault` **çağırmadan** dönüyordu → ikinci
>    basış formu **boş koordinatlarla** natif olarak gönderiyordu. Kayıt kaybı
>    yok (§4.6 güvende) ama **kanıt kaybı** var: `ok` olacak bir tap `flag`'e
>    düşer. Islak/eldivenli parmakta çift basış **beklenen girdi**. Düzeltme:
>    guard artık iptal ediyor, ve `markPending()` basış **kabul edildiği anda**
>    (fix beklenirken) butonu kilitliyor. **Testin sınırı açıkça yazıldı:**
>    repoda JS çalıştırıcı **yok** ve olmayacak (Node yasak §1), o yüzden bu bir
>    **kaynak düzeyinde regresyon ağı** — hatayı üreten tam şekli yakalar, davranışı
>    çalıştırmaz.
> 3. **`internal/domain/tenant` kendi testlerini aldı** (§8): normal yol,
>    **tap edilen** lokasyonun profil lokasyonunu yenmesi, `ErrForeignLocation`'ın
>    **dolu** sonuçla dönmesi + venue adının sızmaması, nil lokasyon, bilinmeyen
>    çalışan, **çapraz-tenant çalışan** — son ikisi **pozitif kontrollü** (aynı
>    id kendi tenant'ında çözülüyor, yani "0 satır"ın sebebi RLS, typo değil).
> 4. **İki ifade düzeltmesi.** (a) *"üç API adı repoda 0 eşleşme"* iddiam
>    **kapsam olarak abartılıydı**; ölçüm R2'nin taradığı yollar için doğru,
>    `CLAUDE.md` §4.2 / `docs/adr/0005` / `docs/plan/*` /
>    `.claude/agents/tappa-security-auditor.md` bu adları **meşru olarak**
>    içeriyor. Kapsam hem karta hem `tap.js`'e yazıldı. (b) `input.css`
>    yorumları **İngilizce'ye çevrildi** (§7 ruhu) — yalnız benim eklediğim blok
>    değil, dosyanın tamamı; yarısı Türkçe bir dosya bırakmak tutarsız olurdu.
>
> **Kullanıcı kararı:** buton **nötr "Tap" kalıyor** — yön karar motorunun işi,
> sayfada tahmin etmek onay ekranıyla çelişirdi. Değişiklik yok.

> **Kart düzeltmesi (2026-07-31, M5-04 3. tur — `tappa-security-auditor` ONAY).**
> Bloklayan yok. Denetçi §4.4'ü **benim ölçümüme hiç güvenmeden** kanıtladı:
> repo dışında, `internal/sun`'dan tek satır almadan bağımsız bir RFC 4493 CMAC
> + NXP SDM türetmesi yazıp **gerçek geçerli bir SUN URL'i** üretti. Canlı: 30
> sayfa açılışı → `last_ctr` **0→0**; in-process: 20 açılış → 700→700, sonra
> **aynı URL `Verify`'dan geçince 700→701 + `SUNValid=true`** (mint'in gerçek
> olduğunun kanıtı), ikinci `Verify` → `false`. Bu, M2-04'ün *"iç-tutarlı vektör
> byte-sırası hatasını yakalayamaz"* dersine karşı **dış bağımsız doğrulama**:
> 1. turda "HTTP yolunda geçerli-CMAC testi yok" diye yazdığım sınır fiilen
> **kapandı** (benim değil, denetçinin ölçümüyle). Beş caydırıcı da kendi
> paketinden derlenerek denendi; beşi de **derleme hatası** verdi.
>
> **Beş bloklamayan bulgu kapatıldı:**
>
> 1. **🔑 Markalı 429 ÜRÜN düzeyinde testsizdi — az önce düzelttiğim hatanın bir
>    alan yanı.** `Refused: t.renderTooManyRequests` → **`nil`** mutasyonu tüm
>    süiti **yeşil** bırakıyordu, çünkü gövdeyi doğrulayan tek test yine
>    `tp.limiter`'ı **kendi kuruyor** ve `Refused`'ı açıkça geçiriyordu. Aynı
>    tuzağın **üçüncü** görünüşü (denetçi 2. turda `Audit` için bulmuştu, ben
>    testimde tekrarlamıştım, şimdi komşu alanda). Düzeltme:
>    `TestTapPage_RefusalBodyIsBrandedAtTheProductWiring` **ürünün kurduğu**
>    limiter'ı üretim adres bütçesiyle sürüyor. **Mutasyon: `Refused: nil` →
>    KIRMIZI.** Ders (tekrar): *bir testin kurduğu şey, o testin
>    doğrulayabileceği şey değildir.*
> 2. **M5-05 kartının akış satırı düzeltildi** — ayrıntı ve doğru sözleşme
>    M5-05 kartındaki kendi düzeltme bloğunda. Özet: POST `sun.Verify`
>    **çağırmaz** (bağlamda CMAC yok, fail-closed), `CMACVerified` **VE** atomik
>    `AdvanceCounter` birlikte SUN geçerliliğini kurar.
> 3. **"tag, ctr ve cmac HTML'de yok" iddiası üç yerde gerçeğe indirildi**
>    (`tap.templ`, `tap_test.go` doküman bloğu, bu kart). Denetçi `ctx`'i çözdü:
>    **uid ve sayaç payload'ın içinde**, yalnız **CMAC** yok. `tapcontext.go`
>    zaten *"The counter does travel"* diyordu → repo kendi kendisiyle
>    çelişiyordu. Sömürülebilir değil (ikisi de adres çubuğunda), ama iddia
>    ölçümden genişti; testin doküman bloğu da artık **ne aradığını** söylüyor.
> 4. **Font boyutu düzeltildi:** 6 woff2 = **79.032 bayt**; 92.126 bayt
>    **dizinin tamamıydı** (2 OFL + README). Hem bu kartta hem — daha önemlisi —
>    **skill `tappa-brand`**'de düzeltildi: skill bir **spec**'tir ve yanlış bir
>    sayı sonraki UI ajanının bütçe kararını bozar.
> 5. **Üç sertleştirme:**
>    (a) **`/static/*` dizin listelemesi kapatıldı.** `GET /static/fonts/`
>        **200 + tam dosya listesi** döndürüyordu (canlı ölçüm); `noListing`
>        sarmalayıcısı artık dizin açılışını `fs.ErrNotExist`'e çeviriyor →
>        FileServer 404 veriyor. Sızan bir şey yoktu (fontlar `app.css`'te zaten
>        adlandırılmış); kazanılan şey, dağıtımın ne taşıdığının ücretsiz
>        envanterini vermemek. **Pozitif kontrollü** test
>        (`/static/js/tap.js` hâlâ 200) + mutasyon KIRMIZI.
>    (b) **Tipli-nil kapatıldı, iddia indirilmedi:** `rec == nil`,
>        `(*audit.Recorder)(nil)` için **false**'tur → `isNil()` (reflect,
>        yalnız başlangıç yolunda). Üretimde erişilemezdi; tam da bu yüzden
>        zayıf kontrol incelemeden geçmişti. Mutasyon (`isNil` → `== nil`)
>        KIRMIZI.
>    (c) **CSP eklendi** — `default-src 'none'` + `script/style/font-src 'self'`
>        + `form-action 'self'` + `base-uri 'none'` + **`frame-ancestors
>        'none'`**. Denetçi enjeksiyon vektörü **bulamadı** (templ escape ediyor,
>        canlı XSS denemesi kaçtı), yani bu derinlik önlemidir — **tek istisna**
>        `frame-ancestors`: bu ekranın tamamı mesai kaydeden tek bir büyük
>        butondur, yani görünmez bir çerçeve altında tıklatmak için ideal
>        hedeftir. **Kapsam:** yalnız bu paketin tap yanıtları; aktivasyon
>        ekranlarına (M5-02) **eklenmedi** — dört denetim turu sürmüş bir akışa
>        yan etki olarak dokunmak yanlış olurdu, kendi görevini hak eder.
>
> **Denetçinin doğrulayamadığı (bulgu değil, dürüstlük payı):** oturumsuz redde
> **0 `audit_log` satırı** iddiasını canlıda üretemedi (adres bütçesi 3000/10dk,
> 3000+ curl orantısızdı); in-process test üretim bütçesini gerçekten sürüyor.

---

## M5-05 — `POST /api/checkin`: orkestrasyon

- **Bağımlılık:** M2-07 · M4-07 · M5-04
- **Kırmızı çizgi:** §4.3 · §4.4 · §4.6 — **milestone'un en kritik görevi**
- **Commit:** `feat(tap): decide and record check-in`

**Amaç.** Dört kanıtı toplayıp karara bağlamak ve kaydı yazmak.

**Akış.** imzalı bağlamı çöz → tag'i **yeniden** çözümle → **`sun.AdvanceCounter`
(atomik)** → bağlam topla (oturum, IP, GPS, vardiya, son işlemler) →
`tap.Decide` → **kaydı yaz** → yanıt üret. Handler'da iş kuralı ve ham SQL
**yok** (§3).

🔴 **`sun.Verify` BU YOLDA ÇAĞRILMAZ** ve çağrılamaz — gerekçesi ve doğru
sözleşme aşağıdaki kart düzeltmesinde.

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

> **Kart düzeltmesi (2026-07-31, M5-04 3. tur denetiminden — M5-05'in akış
> satırı gerçekle çelişiyordu).** Bu kart *"parse → `sun.Verify` → …"* diyordu.
> M5-04 teslim edildikten sonra bu **çalışmaz**, ve denetçi bunu ölçtü:
>
> - Form `action="/api/checkin"`, **query string yok**, gövde yalnız
>   `ctx/lat/lng/acc`; `<meta name="referrer" content="no-referrer">` Referer'ı
>   da siliyor. Yani POST'a ulaşan hiçbir yerde ham SUN URL'i **yok**.
> - İmzalı bağlam UID, `ctr` ve `channel` taşıyor ama **CMAC taşımıyor** —
>   bilinçli (§4.7: çipin MAC'i sayfaya hiç inmiyor, yalnız kontrolün tek bitlik
>   sonucu iniyor). Çözülen örnek payload:
>   `1|6CB832018A53A3|5|nfc|1|<tenant>|<location>|<ts>`.
> - Dolayısıyla CMAC'siz elle kurulmuş bir `Params` ile `sun.Verify` çağrılırsa
>   `verifyMAC` **false** döner → `SUNValid=false` → sayaç **hiç ilerlemez** →
>   her NFC tap `flag`'e düşer. **Fail-closed**, sessiz açık değil — ama §4.4'ün
>   yaşadığı tek yerde bir sonraki ajanı doğaçlamaya iterdi.
>
> **DOĞRU SÖZLEŞME — SUN geçerliliği İKİ yarının VE'sidir:**
>
> ```
> sunValid  ==  ctx.CMACVerified          (M5-04, sayfa kurulurken sunucuda ölçüldü)
>           &&  AdvanceCounter başarılı   (M5-05, ATOMİK — §4.4'ün tek gerçek koruması)
> ```
>
> Birinci yarı **eksiksizdir ve bayatlamaz**: bir CMAC ya tag'in anahtarına göre
> doğrulanır ya doğrulanmaz. İkinci yarı, taze bir dokunuşu **replay**'den
> ayıran **tek** şeydir ve yalnız POST yapabilir. `CMACVerified`'ı okumadan
> sayacı ilerleten bir POST **sahte** tap kaydeder; sayacı ilerletmeden
> `CMACVerified`'a bakan bir POST **replay** kaydeder. İkisi de yazılmalı.
>
> **Uygulama notu:** `sun.AdvanceCounter` zaten **exported**'dır ve tenant
> kapsamlı bir querier ister → `WithTenant(ctx.TagTenantID, …)` içinde çağrılır.
> Tag **yeniden çözümlenmeli** (`GetTagByUID`, bağlamsız): `status` bağlamda
> taşınmıyor ve GET ile POST arasında `lost` işaretlenmiş bir plaket §5 satır
> 1'e düşmeli. Bağlamın `TagTenantID`'si ile taze çözümlemenin tenant'ı
> **karşılaştırılmalı** (N5).
>
> Sözleşme kodda **üç yerde** büyük harfle yazılı — `internal/sun/preview.go`,
> `internal/handler/tapcontext.go`, `internal/handler/tap.go` — eksik olan tek
> şey bu kartın kendi akış satırıydı; düzeltildi.
>
> ⚠️ **QR kanalı için ek uyarı (M5-04 devri):** QR'da `ctr` yoktur, yani
> ilerletilecek sayaç da yoktur — imzalı bir QR bağlamı **TTL boyunca (15 dk)
> tekrar POST edilebilir** ve onu durduran tek şey 60 sn'lik debounce
> guardrail'idir. NFC'de atomik ilerletme bunu yapısal olarak kapatır; QR'da
> kapatmaz. M5-08 bu kanalı açarken bunu bilerek ele almalı.

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

> **Kart düzeltmesi (2026-07-31, M5-05 uygulaması sırasında).** Hiçbir kriter
> düşürülmedi. Kartın **bilmediği bir bloklayıcı ölçüldü** ve kartın anmadığı
> yedi karar aşağıda gerekçesiyle sabitleniyor.
>
> **1) 🔴 ORDINARY BİR CHECK-IN BU GÖREVDEN ÖNCE YAZILAMIYORDU — ölçüldü.**
> *(⚠️ Bu bloğun "conflict hedefi mevcut PK" ifadesi 2. turda düzeltildi — aşağı.)*
> `policy.BaselinePolicies(versionID)` bir callback alır ve dokümanı "üretimde
> **M7-03**'ün kalıcılaştırdığı gerçek id" der; **M7-03 yapılmadı.** Gerçek
> Postgres'e karşı, `tappa_app` olarak, seed'li KF tenant'ı için:
>
> | Deneme | Sonuç |
> |---|---|
> | `policies` / `policy_versions` satır sayısı | **0 / 0** |
> | `policy_layer='baseline'` + rastgele `policy_version_id` | **ERROR 23503** `transactions_policy_version_fk` |
> | `policy_layer='baseline'` + `uuid.Nil` (N4'ün uyardığı wiring bug'ı) | **ERROR 23503** |
> | `policy_layer='guardrail'` + `policy_version_id NULL` | **INSERT 0 1** |
>
> Yani §5'in **1–5. satırları** (guardrail) yazılabiliyordu ama **6. ve 7.
> satırlar** — sıradan `ok` ve sıradan `flag`, ürünün **ana yolu** — 00008'in
> CHECK'i + composite FK'si yüzünden **kaydedilemiyordu**. Bu, §4.6'nın en sık
> gerçekleşen sonuçta ihlali.
>
> **Seçilen yol: (a) M5-05 baseline'ı ilk ihtiyaçta MATERYALİZE EDER.** Reddedilen
> (b) "gürültülü başarısız ol" iki şeyden birine indirgeniyordu: tap'i düşürmek
> (§4.6 ihlali) ya da baseline'sız değerlendirmek — ki o da kaydı yazar ama o
> tenant'taki **her** tap'i fail-to-review default'una düşürür; sessiz olmayan
> ama kalıcı bir bozulma. On satırı bir kez yazmak ikisinden de iyi.
> **Yapısal detaylar:** id'ler `uuid` v5 ile **(tenant, doküman adı)**'ndan
> türetilir → conflict hedefi **mevcut PK**, yani **migration gerekmedi**;
> `ON CONFLICT DO NOTHING` üçünde de (policies / policy_versions / attachments);
> `policy_versions` append-only olduğu için tekrar yazmak **no-op**;
> `enabled=false` olan bir baseline **ne Set'e girer ne de yeniden yaratılır**.
> **M7-03'e kalan:** sign-up anında provisioning, panelin görünümü ve **baseline
> yükseltme akışı** (yeni `BaselineVersion` tenant'a sunulur, kabul edilirse
> version 2 eklenir). Bu dosya **asla var olan bir sürümü değiştirmez**;
> sürümler farklıysa **saklanan** doküman değerlendirilir (kayıt onu pinliyor) ve
> INFO log düşer. **Ölçüm (canlı sunucu, seed'li KF):** `policies 0 → 9`,
> `policy_versions 0 → 9`, sonraki üç tap'te **9 → 9** (idempotent), ve
> `transactions` satırı `layer=baseline` + **gerçek** `policy_version_id` ile
> yazıldı.
>
> **1b) N5'in şekli ölçüldü ve state.md'nin cümlesi bir yerde DÜZELTİLMELİ.**
> "Bugün `ok` yazılır" **kararın** doğru tarifi (saf test: besleme kaldırılınca
> `Decide` → `ok`), ama **yazmanın** değil: yazma yolu, oturumun tenant'ına
> **yabancı bir `tag_uid`** ile INSERT etmeye çalışır ve
> `transactions_tag_fk` bunu **23503** ile reddeder (ölçüldü, hem canlı psql
> hem de üretim koduna uygulanan mutasyonla: unfed çapraz-tenant tap → **HTTP
> 500**, sıfır satır). Yani beslemesiz hâlde sonuç *çapraz-tenant `ok` satırı*
> değil, **kayıp kayıt** (§4.6) olurdu — şemanın composite FK'si sessiz ikinci
> ağdı ve bir izolasyon ihlalini bir kayıt kaybına çeviriyordu. Guardrail
> beslendiğinde sonuç temiz: **403, iki tenant'ta da 0 satır**, ve karar
> açıklanabilir.
>
> **2) `tap.Input` iki alan çifti kazandı; ikisi de ölü guardrail besliyor.**
> `TagTenantID`/`SessionTenantID` (**N5**) ve `OccurredAt`/`OccurredAtFromClient`
> (**K1**). ⚠️ **N5'te bir tuzak vardı ve kapatıldı:** Decide, session tenant'ı
> verilmediğinde değersiz bir **placeholder** kullanıyor; gerçek bir tag tenant'ı
> o placeholder ile karşılaştırılsaydı **var olmayan bir mismatch** üretilir, o da
> **redirect** olduğu için **kayıt yazılmazdı** (§4.6 kaybı). Çözüm: placeholder
> dalında tag tenant'ı Context'e **hiç konmuyor** (guardrail atıl kalıyor —
> M5-05 öncesi davranışın aynısı) ve `checkin.Service` session tenant'sız isteği
> **reddediyor**, yani o dal üretimden erişilemez. `sys:occurred-at-bound`
> **her tap'te** besleniyor (eksik anahtar ≠ false); sıfır `OccurredAt` **"şimdi"**
> demek — düz okunsaydı her eski çağıran 56 yıllık sapmayla **reject** olurdu.
>
> **3) `tap:queued` ile `transactions.queued` AYNI ŞEY DEĞİL** ve karıştırılması
> kolay olduğu için üç yerde yazılı. **Bağlam anahtarı** = "bu tap çevrimdışı
> kuyruktan geldi" (skew'den türetilir, istemci beyanı değil — ADR 0004 §8).
> **Sütun** = "onay kuyruğunda mı" (00005 birebir böyle diyor: `flag -> true`).
>
> **4) `Decision.PolicyContext` eklendi** — Evaluate'e verilen **tam anahtar
> haritası**. Alternatif (yazma yolunun Input'tan yeniden kurması) aynı gerçeği
> **iki yerde** hesaplamaktı; bu repo o hatanın bedelini üç kez ödedi. §4.7:
> harita **mesafe** taşır, koordinat taşımaz (test her anahtarı tarıyor).
>
> **5) Yeni paket `internal/domain/checkin`** (kart bir yer adı vermiyordu).
> `internal/handler` §3 gereği SQL/iş kuralı tutamaz; `internal/domain/tap`
> **saf** kalmak zorunda (pgx import'u saflık kanıtını bitirirdi). Orkestrasyon
> ikisinin arasına yazıldı. **Yeni sorgular:** `GetEmployeeForTap`,
> `GetLocationForTap`, `GetDepartmentShift` (yeni dosya `departments.sql`),
> ve `policies.sql` (`ListPolicySet` + üç `Ensure…`). **Migration YOK.**
>
> **6) Yanıt ekranı: `pages.Result` — ARA ÇÖZÜM, M5-06'nın işi duruyor.** POST bir
> tarayıcı gezinmesidir, yani "karar döndür" = "sayfa göster"; M5-05 bunu
> atlayamaz. Teslim edilen: damga (`APPROVED/FLAGGED/REJECTED/IGNORED` — renk
> **tek başına** durum anlatmıyor), mono saat, **buton yok** (§9: "All done").
> M5-06'ya kalan: tenant mesajı, kopya turu, docket'in tam işlenişi.
>
> **7) `POST /api/checkin` `Tap.Mount` grubuna, AYNI limiter örneğiyle** girdi
> (`ByAddress → Identify → BySession`). Bir tap **iki istektir**; ayrı bütçeler
> hiçbirinin bir tap'i tarif etmemesi demekti. **Devralınan 429 kalıntısı
> (M5-03 md.2) ÇÖZÜLMEDİ** ve çözüldüğü iddia edilmiyor.
>
> **Açıkta bırakılan sınırlar (iddia değil, sınır):**
> - **Eşzamanlılık testinin gücü sınırlı** *(⚠️ bu cümle 3. turda hem düzeltildi
>   hem kapatıldı — aşağıki 3. tur bloğu, madde 1)*. 12 goroutine + start
>   bariyeri ile atomik uygulama **tam 1** kazanan veriyor; ama negatif kontrol
>   (oku-sonra-yaz) **penceresi 80 ms genişletilmeden** testi kırmıyor — istekler
>   havuzda sıralanıyor. Genişletilince **12/12** kazanan. **Bu satır "o test
>   kırılmıyor" diyordu; ölçüm "HİÇBİR test kırılmıyordu" idi** — açığı kapatan
>   şey artık çekişme değil, **çağrının pinlenmesi** (`counterAdvancer`).
> - **Oturumsuz bir POST tag'e bağlanamaz.** İmzalı bağlam **oturum id'si**
>   üzerinden MAC'lidir → oturum yoksa doğrulanacak anahtar yok. 00005
>   `employee_id`'yi tam da kimliksiz bir reddi kaydedebilmek için nullable
>   bırakıyor; o şekil bugün **erişilemez** (GET /t zaten oturumsuzu yönlendiriyor).
>   Şema izin veriyor, akış üretemiyor — **sınır olarak yazıldı**.
> - **`acc` (GPS doğruluğu) okunmuyor:** sütunu yok, kuralı yok. 5 km doğruluklu
>   bir "fix" 150 m yarıçapta aynı sayılıyor. Kendi görevini hak ediyor.
> - **Tenant katmanı bugün BOŞ** — `db/queries`'te tenant policy yazan sorgu yok
>   (panel M6-09). Yükleyici yine de okuyor; pozitif kontrol testte.
> - **Debounce temeli herhangi bir verdict'tir** (`GetLastTransactionForEmployee`
>   sorgusunun kendi sözleşmesi). Sonuç: 10 sn önce başka bir plakette `reject`
>   almış biri gerçek tap'inde `ignored` alabilir, ve sabırsız tapper kendi
>   penceresini uzatır. Kartın "60 sn içinde tekrar" ifadesiyle uyumlu; farklı
>   isteniyorsa politika kararıdır.
> - **`tap:trust` / `tap:direction` / `tap:practice` / `time:minutesLate`
>   bağlamda YOK** (M4'ten devraldığım durum): bunlar Evaluate'ten **sonra**
>   hesaplanıyor, yani bu anahtarlara yazılmış bir tenant politikası **sessizce
>   hiç eşleşmez**. M5-05 bunu kötüleştirmedi ama düzeltmedi de — M6-09 politika
>   yazma yüzeyi gelmeden önce ele alınmalı.

---

> **Kart düzeltmesi (2026-07-31, M5-05 2. tur — üçüncü göz ONAY, dört bulgu
> düzeltildi).** Denetçi bloklayan bulmadı ve çok şeyi kendi komutuyla doğruladı
> (§5 satır 1/3/6/7, N5'in 403 + iki tenant'ta 0 satır oluşu, 8 eşzamanlı ilk
> tap'te fazla satır olmaması, 15 sahte form alanının yok sayılması, 40 reject →
> 40 tx / 40 audit 1:1, `make gen`'in 17 üretilmiş dosyayı bayt bayt aynı
> bırakması). Dört bulgu düzeltildi, ikisi yazıldı.
>
> **🔑 F1 — çapraz-tenant tap YABANCI tenant'ın sayacını ilerletiyordu.** Ölçüm:
> yabancı plaket `last_ctr` **900 → 901**, iki tenant'ta da **0** `transactions`
> ve **0** audit satırı. Sebep sıraydı: `advance`, `Decide`'dan **önce** ve
> **`WithTenant(tag'in tenant'ı)`** ile — yani **öteki tenant'ın RLS bağlamında**
> — çalışıyordu. Bir tenant'ın oturumu ötekinin satırını **izsiz** değiştiriyordu
> (§4.5). Zarar dar ama gerçek: sayaç yalnız çipin gerçekten ürettiği değere
> gidebilir, ama o okumayı **sahibinin altından harcar** — aynı okuma için sayfası
> açık duran meşru çalışan POST ettiğinde **kaydedilmiş replay-reject** alır.
> **Düzeltme:** `advance`'e dördüncü koşul — `tagRow.TenantID != SessionTenantID`
> → hiç dokunma. Bu bir **kapsam** kuralıdır ("ait olmadığın tenant'a yazma"),
> karar değil: verdict hâlâ `sys:tenant-mismatch`'in ve `Decide` yine koşuyor.
> **Mutasyon:** koşulu silince yeni test `900 → 901` ile **RED**; pozitif kontrol
> (aynı plaket, kendi tenant'ının oturumu) **900 → 901 beklendiği gibi**.
>
> **🔑 F2 — `sys:tap-freshness` (guardrail #4) ÖLÜYDÜ.** `tap:pageAgeSeconds`'ı
> ürün genelinde **hiçbir şey** set etmiyordu; eksik anahtar hiç eşleşmez, yani
> guardrail hiçbir pencerede ateşleyemezdi. **Seçim: BESLE** (kartın önerdiği
> ikinci yol — "TTL kapsıyor diye yaz" — reddedildi, çünkü iki cevap **türce**
> farklı: TTL **kaydedilmeyen 400**, guardrail **kaydedilen reject** üretir ve
> §4.6 ikincisini ister). `tap.Input.PageIssuedAt` eklendi; kaynak imzalı
> bağlamın `issuedAt`'i (sunucu saati, authenticated — istemci seçemez).
> **Ölçülen sınır, iddia değil:** bugün `tapContextTTL = 15dk` **==**
> `FreshnessMaxSeconds = 900 sn`, yani guardrail'in bandı **boş** — uçtan uca
> ölçüldü: 14dk59sn → **200 + kayıt**, 15dk01sn → **400, +0 satır, sayaç sabit**.
> Yani besleme bugün davranışı değiştirmiyor; değiştirdiği şey M5-10 pencereyi
> daralttığı anda (1–15 dk, varsayılan 3) o bandın **kayıtla** cevaplanabilir
> olması. Guardrail'in artık ateşleyebildiği **ölçüldü** (pencere 60 sn'ye
> çekilip 5 dk'lık sayfa → `reject`/`sys:tap-freshness`), ve **beslemesiz**
> mutasyon aynı girdide `ok` veriyor (testte).
>
> **🔑 F3 — N3 kabloluydu ama mutasyonu YEŞİL kalıyordu.** Harness 60 sn
> yapılandırıyordu ve `policy.DefaultParams()` de 60 sn — yani
> `params.DebounceWindow = cfg.Debounce` **silinince suite yeşil** kalıyordu:
> M5-04 RED'inin ikinci kostümü, "kendi kurduğu nesne" yerine **dejenere değer**.
> Harness **120 sn**'ye alındı (ADR 0004 §11 aralığında, varsayılandan farklı) ve
> yeni test 60 ile 120 arasındaki bandı sürüyor: 90 sn'lik boşluk → `ignored`.
> **Mutasyon artık RED** (`ok`/`base:ip-or-gps-ok` veriyor). Teste ayrıca bir
> koruma kondu: harness değeri bir gün varsayılana döndürülürse test **kendini
> anlamsız bulup patlıyor**.
>
> **🔑 F4 — dört verdict damga sınıfı derlenen CSS'e HİÇ girmiyordu.**
> `StampClass()` sınıf adını **Go'da** kuruyordu; Tailwind `content` globları
> `web/templates/**/*.templ` + `web/static/js/**/*.js` — **Go taranmıyor**. Ölçüm
> (taze `make css`): `stamp--approved|flagged|rejected|ignored` = **0/0/0/0**,
> yani APPROVED/FLAGGED/REJECTED/IGNORED hepsi çıplak `.stamp` ile render
> oluyordu — metin durumu taşıdığı için **erişilebilirlik bozulmuyor**, ama §9'un
> sabit durum→renk eşlemesi bozuluyordu. **Düzeltme: literaller `.templ`'e
> taşındı** (`templ stamp(v)` bileşeni). Glob'a `**/*.go` eklemek ve safelist
> **reddedildi**: ikisi de sınıf adını aracın **haberdar edilmesi gereken** bir
> dosyada bırakır ve bu tam da kırılma sebebiydi. **Ölçüm sonrası:** 1/2/2/2 ve
> kurallar doğru renkleri taşıyor (`rgb(31 92 65)` = tappa-green,
> `rgb(190 61 42)` = tomato). Counterfactual da ölçüldü: sınıfı Go tarafına geri
> koyup derleyince yine **0/0/0/0**. (`app.css` gitignore'da — üretilen dosya;
> bu yüzden bulgu ancak **taze derlemeyle** görülür.)
>
> **F6 — kart ifadesi düzeltildi (üç yerde).** "conflict hedefi **mevcut PK**"
> yalnız `policies` için doğru. Gerçek: `policies` → **PRIMARY KEY (id)** ·
> `policy_versions` → **`policy_versions_no_key` UNIQUE (tenant_id, policy_id,
> version_no)** · `policy_attachments` → **`policy_attachments_resource_key`
> UNIQUE (tenant_id, policy_id, resource)`**. Öz aynı kalıyor (migration
> gerekmedi, var olan kısıtlar kullanıldı), ifade artık doğru. Aynı yanlış cümle
> `db/queries/policies.sql` ve `policyset.go` başlıklarında da düzeltildi.
>
> **Yeni sınır (denetçinin 7. devri):** `Record` baştan sona `r.Context()`
> taşıyor → istemci **advance ile insert arasında** bağlantıyı keserse sayaç
> harcanır, kayıt yazılmaz. Kalıcı kayıp değil (yeni fiziksel dokunuş daha yüksek
> bir `ctr` üretir) ama o basış için kanıt gider ve kimse söylemez. Yazmayı
> iptalden ayırmak **daha kötü** bir sorun üretirdi (terk edilmiş bir isteğin
> mesai kaydetmesi, üstelik ayırt edilemez), o yüzden olduğu gibi bırakıldı ve
> `checkin.go`'da adıyla yazıldı. Pencere birkaç milisaniyelik yerel DB işi.
> *(⚠️ **Düzeltme, 2026-08-01, M5-08:** bu son cümle artık **yanlış**. ADR 0006'nın
> kişi başına advisory kilidi `write`'ı sınırsız bir kilit beklemesinin arkasına
> taşıdı; pencere artık `advance` → havuz alımı → **kilit beklemesi** → `INSERT`
> ve tavanı `middleware.Timeout(30s)`. Ölçüldü: dışarıdan 3 sn tutulan kilitte tap
> 3,32 sn, tek kişiye 50 eşzamanlı POST'ta en kötü 1,24–1,91 sn. Kayıp **şekli**
> değişmedi ve sessiz değil; değişen **süresi**. Blok tarihsel kayıt olarak
> duruyor, doğrusu `internal/domain/checkin/checkin.go`'nun `Record` başlığında.)*
>
> **Düzeltilen bir aşırı-iddia:** `checkin.go`'daki *"It REDIRECTS and writes
> nothing"* cümlesi `transactions` için doğru, **mutlak hâliyle yanlıştı** (F1
> tam da onu çürüttü). Artık garanti edilenin boyunda: *hiçbir tenant'ta
> `transactions` satırı yok; **yabancı** tenant'ın hiçbir durumu değişmiyor* —
> ve **oturumun KENDİ** tenant'ında baseline materyalizasyonu olabileceği açıkça
> yazıldı (karar kurallar olmadan verilemez).

---

> **Kart düzeltmesi (2026-07-31, M5-05 3. tur — `tappa-security-auditor` ONAY).**
> Bloklayan yok. Denetçi kendi sondalarıyla çok şeyi doğruladı (§5'in altı satırı,
> N5'in 403 + iki tenant'ta 0 satır + yabancı `last_ctr` sabit + gövdede sızıntı
> yok, dokuz düşmanca `occurred_at`/GPS girdisi → hepsi **200 + tam satır, hiç
> 500 yok**, politika katmanı çökerken bile kayıt yazılıyor, materyalizasyon
> `max version_no = 1`, `log_statement=all` altında **tek ifade**, R1/R2/R7
> temiz). Üç bulgu düzeltildi, iki sınır yazıldı, biri zaten sınırdı.
>
> **🔑 1 — ÜRETİM YAZMA YOLUNDAKİ GERÇEK BİR TOCTOU TÜM SUITE'İ YEŞİL BIRAKIYORDU
> (bu sınıf ÜÇÜNCÜ kez).** Denetçi atomik ilerletmeyi aynı `WithTenant` içinde
> `SELECT last_ctr` → Go'da karşılaştır → `UPDATE`'e çevirdi: `make test -race`
> → **13 paketin HEPSİ ok**. Aynı mutasyon + 80 ms uyku → **"12 of 12 concurrent
> taps were SUN-valid"**, yani mutasyon **gerçek bir replay deliği**. Sebep iki
> kanıtın arasındaki boşluktu: `internal/sun` **`AdvanceCounter`'ı** kanıtlıyor,
> bu paket **kaydın doğruluğunu** kanıtlıyor, **çağrının varlığını hiçbir şey
> kanıtlamıyordu**.
> **Seçilen yol: ÇAĞRIYI PİNLE** (denetçinin ilk seçeneği). Çekişmeyi bu katmanda
> test etmek işe yaramazdı ve bu **ölçüldü**: SELECT→UPDATE penceresi milisaniye
> altı, HTTP testi bariyerli ve bariyersiz iki kez bozuk şekle karşı **yeşil**
> kaldı. Eklenen: tüketici tarafında `counterAdvancer` arayüzü + `Service.advancer`
> tohumu (üretim `storeAdvancer` = `store.New`). Yeni testler
> `internal/domain/checkin/advance_test.go`: **tam 1** `AdvanceTagCounter`, doğru
> tenant/uid/ctr ile, **tag'in tenant'ının** `WithTenant`'ı içinde, ve
> transaction'a **kendi başına 0 ifade**; ayrıca ilerletmeyi durduran **altı
> koşulun** her biri sorguyu **hiç açtırmıyor**. **Mutasyon artık RED:**
> *"AdvanceTagCounter was called 0 times, want exactly 1"*. `New`'in gerçek
> querier'ı bağladığı da ayrıca test ediliyor — tohum, gerçek sorgunun kaybolduğu
> yer olmasın diye.
>
> **🔑 2 — §4.6 başlığı "TEK istisna" diyordu; ölçüm ÜÇ.** Paket başlığı
> düzeltildi: (a) §5 satır 3 (oturum yok — bu pakete hiç ulaşmıyor), (b)
> `sys:tenant-mismatch` redirect'i, (c) bilinmeyen uid (yalnız `audit_log`).
> Aynı cümledeki *"burada tek INSERT var"* da yanlıştı — `policyset.go`'nun üç
> INSERT'ü ve `audit_log` var; artık *"`transactions`'a yazan tek ifade"* diyor.
> Kodun kendisi doğruydu; yanlış olan **başlıktaki garanti beyanıydı** — bu
> oturumun sekiz RED'ini üreten sınıf.
>
> **🔑 3 — sıfır-zaman nöbetçisi geçerli bir RFC 3339 değeriyle çakışıyordu.**
> Ölçüm (bir saniye arayla, zıt sonuçlar): `0001-01-01T00:00:00Z` → **`ok`**, ve
> satıra **SUNUCU SAATİ** yazılıyor; `0001-01-01T00:00:01Z` → **`reject` /
> `sys:occurred-at-bound`**, satıra `0001-01-01`. Zarar yönü saldırganı
> güçsüzleştiriyordu ve kayıt yazılıyordu, ama `handler/checkin.go` *"beyan
> edilmiş bir zaman sessizce başkasıyla damgalanmaz"* diyordu ve **bu değer için
> yanlıştı**. **Seçim: NÖBETÇİYİ AYIR, iddiayı indirme.** Gerekçe: iddia
> korunmaya değer bir özellik ve maliyeti tek bir pointer —
> `checkin.Request.OccurredAt` artık `*time.Time` (nil = "sunucu zamanlar"), ve
> `tap`'te gerçek **`OccurredAtFromClient` bayrağından** okunuyor, sıfır-değer
> testinden değil. Bir form alanı `nil` taşıyamaz, yani iki soru ("beyan edildi
> mi", "değeri ne") artık bir değeri paylaşmıyor. **Sonra:** üç yazım da
> (`…00Z`, `…01Z`, `+01:00` ofsetli) **`reject`/`sys:occurred-at-bound`**, satırda
> **beyan edilen yıl 1**; pozitif kontrol (alan yok) → sıradan tap, sunucu saati.
>
> **Yazılan iki sınır (kod değişmedi):**
> 1. **Sayaç kayıttan ÖNCE harcanıyor; advance ile insert arasındaki HER hata o
>    basışın kanıtını götürür.** Kart yalnız **iptal** şeklini yazmıştı; denetçi
>    **DB hatası** şeklini ölçtü (çözülemeyen employee id → `transactions` **+0**,
>    `last_ctr` **700→701**). Sınır ikisini de kapsayacak şekilde genişletildi.
>    Bugün erişilebilirliği dar: `tappa_app` `tags`/`employees` silemiyor, yani
>    çağıran bu hatayı **üretemiyor**; kalıcı kayıp da değil (yeni dokunuş daha
>    yüksek `ctr` üretir) ve **sessiz değil** (ERROR log + *"Nothing was
>    recorded … Tap the plaque again in a moment."*).
> 2. **`transactions` artık bir YAZMA BÜTÇESİDİR.** Ölçüm: tek oturum + tek mint
>    edilmiş bağlam + 40 POST → **40 silinemez satır**; bütçelerle (300/10dk
>    oturum, 3000/10dk adres) oturum başına ~43.200, mekân adresi başına ~432.000
>    satır/gün. **Sahtecilik değil** — her satır atfedilebilir ve çoğu `ignored`
>    veya `reject` — **gürültü ve depolama**. Yazıldı çünkü M5-02'nin *"koruma
>    maliyeti, koruduğu şeye saldırı olmasın"* argümanı **yalnız `audit_log`
>    için** kurulmuştu. Çözüm 429 kalıntısıyla aynı: paylaşılan store +
>    mekân-başına anahtarlama (**M8**).
>    ⚠️ `audit_log` tarafı denetçide **temiz**: `tap.security_alert` 1:1;
>    `tap.unknown_tag` **çözülemeyen bir uid** istiyor ve `tappa_app`'in `tags`
>    üzerinde DELETE'i yok (ölçülen grant: `INSERT,SELECT,UPDATE`) → çağıran
>    üretemiyor.
>
> **Sınır olarak bırakılan (denetçi "delik değil" dedi):** `Origin: null` kabul
> ediliyor. Artık `handler/checkin.go`'da **adıyla** yazılı: sandbox'lı iframe,
> `data:` doküman ve bazı gizlilik araçları bunu gönderir; reddetmek meşru
> çağıranı kırar, saldırgan ise başlığı hiç göndermezdi. Asıl savunma zaten
> **oturum-bağlı MAC + SameSite=Lax**.

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

> **Kart düzeltmesi (2026-07-31, M5-06 uygulaması sırasında).** Üç kriter
> ölçüldükten sonra daraltıldı ya da netleştirildi; üçü de kartın *ruhunu*
> koruyor ama *lafzını* değiştiriyor.
>
> **1. "Başarısızda Try again var" → ALTI ekrandan YALNIZ BİRİNDE buton var.**
> §9'un asıl kuralı hiçbir ekranın ikinci bir mesai kaydı üretememesidir; bu
> yüzden bu akışta "Try again"in tek dürüst anlamı *"bu sayfayı yeniden getir"*
> ve o da yalnız hata GEÇİCİ ise ve yeniden getirilecek adres **hiçbir şey
> yazmayan bir GET** ise işe yarar. Ölçülen tablo:
>
> | Ekran | Nereden | Affordance | Hedef URL | Kayıt üretir mi | `last_ctr` ilerletir mi |
> |---|---|---|---|---|---|
> | `tapProblemServer` | **GET `/t`** — **5 daldan 3'ü** (identity-unresolved · sun pre-check · directory) | **link: "Try again"** | isteğin kendi URI'si (`/t?tag=…&ctr=…&cmac=…`) | **hayır** | **hayır** |
> | `tapProblemServer` | GET `/t` — `mint` ve unknown-session-state dalları | talimat | — | hayır | hayır |
> | `tapProblemServer` | POST | talimat | — (verilecek adres yok) | hayır | *zaten harcanmış olabilir* (devir md.2) |
> | `tapProblemBadURL` | GET + POST | talimat | — | hayır | hayır |
> | `tapProblemUnknownTag` | GET + POST | talimat | — | hayır (POST'ta audit satırı) | hayır |
> | `tapProblemTooMany` | middleware | talimat | — | hayır | hayır |
> | `tapProblemStale` | POST | talimat | — | hayır | hayır |
> | `tapProblemForeignTenant` | POST | talimat | — | hayır | hayır |
>
> Gerekçeler: bozuk URL yeniden getirilince **aynı** hatayı verir · bilinmeyen
> plaket bilinmeyen kalır · 429'a buton koymak, sayfanın *durmasını istediği*
> bütçeyi harcatır · POST tarafında verilecek adres **yoktur** (tap bağlamı tek
> kullanımlık, çipin SUN URL'i istekte yok). Buton konmayan yerde ekran **ne
> yapılacağını** söylüyor (marka kuralı). Affordance `<a>`'dır, `<form>`/`<button>`
> değil — bir link POST gövdesini yeniden gönderemez, yani bu paketteki hiçbir
> hata ekranı ikinci bir `transactions` satırı üretemez. GET `/t`'nin hiçbir şey
> yazmadığı ve sayacı ilerletmediği zaten `TestTapDB_PageNeverMovesTheCounter`
> ile gerçek Postgres'e karşı (pozitif kontrollü) ölçülü.
>
> **2. Tenant'a özel mesaj bugün TENANT'a değil, `tenants.business_type`'a
> bağlı.** Şemada tenant başına düzenlenebilir metin sütunu yok ve M5-06 yeni
> migration açmıyor; seed UUID'si üretim koduna girmez. Sonuç: `restaurant` → KF
> metinleri, `production` → KM metinleri, **diğer altı tip + boş değer → nötr bir
> varsayılan** (sessizlik de yok, uydurma sektör jargonu da yok). **Bilinen sınır:**
> ikinci bir restoran tenant'ı KF'in cümlesini görür. Tenant başına metin **M9-04**.
>
> **3. Kartta yazmayan metin kararları** (hepsi §4.6'dan çıkıyor, teslimde ayrıca
> bildirildi): (a) `flag` ekranı artık *"All done"* **demiyor** — kayıt alındı ama
> müdür onaylayana kadar saymıyor, ve bu çalışana söyleniyor; ekran yine
> **butonsuz**. (b) Marka mesajı yalnız `ok`'ta ve **practice olmayan** tap'te
> çıkıyor: eğitim tap'i vardiya başlangıcı değil. (c) `practice && flag`
> bileşiminde `flag` cümlesinden *"before it counts"* **çıkarıldı** — practice
> zaten hiçbir koşulda saate girmiyor, onay bunu değiştirmiyor; iki cümle
> birbirini yalanlamasın.
>
> **4. `ignored` ekranı ÖNCEKİ tap hakkında hiçbir şey iddia etmiyor** (kırmızı
> çizgi denetiminde bulundu, 2. turda düzeltildi, 3. turda **sertleştirildi**).
> İlk sürüm *"Your earlier tap stands."* diyordu; bu **yanlış**, çünkü debounce
> **verdict'ten VE KANALDAN bağımsız**: `GetLastTransactionForEmployee`'de ne verdict
> ne de `channel` yüklemi var
> → `SecondsSincePersonLastTap` koşulsuz doluyor → `sys:person-debounce` yalnız
> gap'e bakıyor. Yani `ignored`'ın öncülü onay kuyruğunda bekleyen bir `flag` ya da
> bir `reject` olabilir; ekran "kaydedildi" demiş oluyordu. **Sonuç: `flag`'den
> silinen sessiz onay kusuru `ignored`'a taşınmıştı.** İkinci turda *"not counted
> **again**"* yazılmıştı — "again" da bir **önvarsayım** taşıyor (önceden bir
> sayma olduğunu ima ediyor), o da kaldırıldı. `ResultView`'a öncül verdict'i
> **taşınmadı** (§9).
>
> Kanal yüklemi de olmadığı için §5'in `channel='manual'` satırı (müdürün elle
> girdiği kayıt) pencereye düşerse ekran *"You tapped a moment ago"* der, oysa kişi
> tap etmemiştir. Bugün üretilemiyor (manuel giriş M6-04), ama cümle eksikti.
>
> **Koruma biçimi iki kez değişti; bağlayıcı olan üçüncüsü.** (a) 2. turun "yasak
> ifade listesi" testi ölçümle yetersiz çıktı — *"…**and that one is safely logged**,
> …"* mutasyonu **suite'i yeşil bıraktı**; bir *anlamın* yokluğu kara listeyle
> kanıtlanamaz. (b) 3. turun bayt-golden'ı da yetersiz çıktı, ama başka sebepten:
> **elde kurulmuştu ve `Note` boş bir gövdeyi pinliyordu.** Oysa `Decision.Note`
> her zaman dolu (kararı veren kuralın `Reason`'ı; her guardrail/baseline ve
> no-match fallback bir tane taşıyor) — ölçüldü: gerçek `ignored` `<main>` **1061
> bayt**, golden **971 bayt**, aradaki fark tam da Note paragrafı; oraya gizlenen
> bir cümle testi geçti. (c) 4. turun `<main>`-kapsamlı **metin** karşılaştırması da
> **dar** çıktı: denetçi aynı cümleyi `<main>` DIŞINA (ortak kabuk), **öznitelik
> içine** (`title`, `aria-label`), sayfa `<title>`'ına ve **CSS `content:`**'ine
> koydu — **beşi de yeşil kaldı**. (d) **Bağlayıcı olan:** karşılaştırma artık
> **tüm belgenin** metin düğümleri (`<title>` + `<body>`) **artı** insanın
> algıladığı özniteliklerin değerleri — liste `textAttrRE`'de yazılı ve
> **`value` ile `aria-roledescription` dâhil**: ikisi de ilk sürümde eksikti ve
> denetçi ikisinden de aynı §4.6 cümlesini geri soktu (`value` "makineye dönük"
> diye dışlanmıştı; oysa `readonly` bir `<input>` onu ekrana basar). **Liste bir
> yarış olduğu için tek katman değil** — ve ikinci katman bir kez **yeniden
> kuruldu**. Önce kara listeydi (`<input>/<textarea>/<select>/<button>/<form>`) ve
> *"özniteliği gösteren elemanın kendisi kalkıyor"* diye tarif edilmişti; bu
> **yanlıştı**: denetçi `<iframe srcdoc>`, `<object data="data:text/html,…">` ve
> `<img src="data:image/svg+xml,…<text>…">` ile aynı cümleyi üç kez geri soktu,
> dördüncüsünde de kartın **YALAN** olarak belgelediği *"Nothing was recorded and
> nothing was logged"* cümlesini **ortak `Problem` şablonuna** koydu (on bir
> ekranın hepsi) — **dördü de suite'i yeşil bıraktı.**
> *(Düzeltme, 8. tur: o cümlenin ilk hâli "bu ekranlarda CSP yok" diyordu — **yanlış**.
> `handler.Tap.render` yazdığı **her** yanıta CSP koyuyor: `pages.Tap`, `pages.Result`
> **ve** `pages.Problem`. Politikasız olan **beş aktivasyon** hata ekranı
> (`Activation.render`). Ortak şablon ikisine birden düştüğü için testler on bir
> ekranı da koruyor, başlık altısını.)* **Bugünkü
> hâli KAPALI KÜME:** `TestScreens_RenderOnlyTheseElements` iki ailenin render
> ettiği etiket kümesinin **şablonlardan ölçülen kümeye eşit** olduğunu iddia
> ediyor (onay ekranları **16** etiket, hata ekranları **14**). Eleman eklemek de,
> listede kalıp render edilmeyen ölü giriş bırakmak da testi kırar. Kara liste her
> tur bir eleman daha bulunmasına açıktı; kapalı küme **eleman kanalını** bitirir.
>
> **Ama "bitmiş" demek yetmedi (8. tur).** İzinli bir elemanın **okunmayan bir
> özniteliği** kaldı: `link` her iki kümede de var (kabuğun stylesheet'i) ve `href`
> `textAttrRE`'nin **açık dışlama listesinde**. Denetçi
> `<link rel="stylesheet" href="data:text/css,…{content:'…'}">` ile aynı cümleyi hem
> sonuç ekranına hem **ortak `Problem` şablonuna** geri soktu — **ikisi de yeşil**;
> `TestCompiledCSS_GeneratesNoText` de göremez, çünkü `data:` stylesheet hiç
> derlenmiyor. **Üçüncü kapalı küme eklendi:** `TestScreens_ReferenceOnlyOurOwnAssets`
> render edilen belgelerdeki `href`/`src` **değer kümesini** birebir pinliyor
> (onay ekranları `{/static/css/app.css}`, hata ekranları retry linkiyle birlikte) ve
> şema taşıyan her değeri reddediyor. Bu aynı zamanda **markanın "hiçbir dış kaynağa
> runtime bağlantı yok, mutlak URL sayısı 0" kuralını ilk kez teste bağlıyor** — o
> kural bugüne kadar yalnız disiplinle duruyordu. **Dürüst özet:** üç dar beyaz liste
> (metin · eleman · referans) birlikte, hiçbirinin tek başına kapsamadığından
> fazlasını kapsıyor — ama **hiçbiri "bitmiş" değil**, ve 9. tur bunu bir kez daha
> gösterdi: `meta` her iki eleman kümesinde izinli olduğu için
> `<meta http-equiv="refresh" content="30;url=https://…">` **kabuğa konup her
> ekrandan üçüncü tarafa navigasyon** üretiyordu ve suite yeşildi; ayrıca metin
> testi altı hata ekranını **yalnız `RetryURL == ""`** ile render ediyordu, oysa üç
> GET dalı retry linkiyle servis ediliyor — o blok içine konan iki yalan cümle de
> yeşil geçti. İkisi de kapatıldı (meta-refresh `assertRefs`'te reddediliyor;
> metin testi artık `{"", retryURL}` üzerinde dönüyor ve anchor metnini de pinliyor).
>
> **Ayrıca CSP artık pinli.** `grep -rn "Content-Security-Policy"` yalnız tap
> **sayfasını** çeken tek bir teste çıkıyordu; sonuç ekranı ve altı hata ekranı
> denetimsiz bir başlığa yaslanıyordu. `TestTapResponses_CarryTheContentSecurityPolicy`
> **yedisini de** pinliyor — ama 13. tura kadar **altısını** pinliyordu:
> `tapProblemForeignTenant` vakası eksikti (`checkin.go` 403 dalı). Vaka eklendi;
> mutasyonla kanıtlandı (o dal `Tap.render`'ı atlayacak şekilde değiştirilince
> **RED**, `Content-Security-Policy = ""`). Kapsam **11 şekil** (dört verdict +
> bilinmeyen verdict + practice bileşimleri + Note'un dört hâli: boş · tek ·
> reason+`"; "`+stale · **stale tek başına**); **altı `tapProblem*` ekranı ve
> ortak `Problem` şablonu** da aynı disiplinde (bunlar da
> korumasızdı: 429 ekranına *"nothing was logged"* eklendi ve suite yeşil kaldı —
> üstelik o cümle **yanlış**, çözülmüş oturumlu 429 `audit_log`'a satır yazıyor);
> ve hepsi **iki kez** doğrulanıyor — biri fake'lerle, biri **gerçek Postgres'te
> gerçek karar motoruyla** (`TestCheckinDB_ScreenTextIsWhatProductionRenders`).
>
> **SINIR — garanti değil.** Bu mekanizma **CSS ile üretilen metni görmez**:
> `content:` bir `::before`/`::after` üzerinde ekrana gerçek kelime basar ve
> hiçbir HTML okuması onu bulamaz. O kanal **ayrı ve dar** bir kontrolle kapatıldı
> (`TestCompiledCSS_GeneratesNoText` derlenmiş `app.css`'i okur, harf/rakam içeren
> her `content:` bildirimini reddeder), ama **CI'DA HİÇ KOŞMUYOR — DAİMA SKIP.**
> Ölçüldü: `.github/workflows/ci.yml` yalnız `make tools` · `make up` ·
> `make check` · `make audit` koşuyor, hiçbiri CSS derlemiyor, ve `.gitignore:16`
> `/web/static/css/app.css`'i checkout dışında tutuyor. Yani bu kanal **yalnız
> geliştirici makinesinde, elle `make css` sonrası** korunuyor; **atlanması geçme
> değildir.** (CI'a `make css` adımı bu görevin dışında.) Ayrıca tarama, kanıt
> değil.
>
> **İki SINIR daha, garanti değil:** (i) `<meta name="description">` içine konan
> bir cümle metin karşılaştırmasına görünmüyor (ölçüldü, yeşil kaldı) — kullanıcıya
> render edilmediği için kapatılmadı, **sınır olarak yazıldı**. (ii) **Kapsam
> ekran başına ve elle**: bu pakete sonradan eklenen bir şablon, biri onu hem
> metin tablosuna hem etiket kümesine yazmadıkça **korunmuyor**. Bunu zorlayan
> hiçbir mekanizma yok; daha önce doğruydu ama **hiçbir yerde yazılı değildi**.
>
> ### Bilinen açık kanallar (9. tur kapanışı — "kapandı" değil, **sayıldı**)
>
> Bu görevde mekanizma eklemek burada durdu. Kapatılmayan her kanal **garanti
> olarak değil, liste olarak** taşınıyor; kaynak liste `screenText`'in yorumunda,
> özet burada:
>
> 1. **`<meta name="description">`** — ölçüldü, yeşil kalıyor. Kullanıcıya render
>    edilmediği için kapatılmadı. (`http-equiv` taşıyan **her** `<meta>` ayrı:
>    `assertRefs` **öznitelik sırasından bağımsız** olarak reddediyor. 10. turda
>    bu kontrolün kendisi sıra-bağımlıydı — `content=` önce yazılınca geçiyordu,
>    ve HTML'de sıranın anlamı yok; kanonikleştirme artık tek yerde: *"bu meta
>    `http-equiv` taşıyor mu"*. **Uçtan uca (`.templ` → `make gen` → `make test`)
>    ölçülen altı yazımın altısı da RED:** ters sıra · büyük harf
>    (`HTTP-EQUIV="REFRESH"` — templ bunu **birebir koruyor**, `make templ` exit 0,
>    üretilen literalde `HTTP-EQUIV` aynen duruyor, yani bu satır kontrolün
>    gerçekten harf-duyarsız olduğunu sınıyor) · araya fazladan öznitelik ·
>    `=` çevresinde boşluk · `content` hiç yok · başka bir direktif.
>    **Kalan tek varsayım:** `metaTagRE` etiketi ilk `>`'de bitiriyor, HTML ise
>    tırnak içindeki `>`'de bitirmiyor. `.templ` yolundan **üretilemiyor** (templ
>    `>` → `&gt;`), tek erişim yolu elle düzenlenmiş `*_templ.go` — o da md.5.)
> 2. **CSS `content:`** — yalnız geliştirici makinesinde. `TestCompiledCSS_GeneratesNoText`
>    derlenmiş `app.css`'i okur; **CI hiç CSS derlemiyor → daima SKIP**, ve
>    atlanması geçme değildir.
> 3. **Kapsam ekran başına ve elle** — bu pakete sonradan eklenen bir şablon, biri
>    onu hem metin tablosuna hem eleman kümesine yazmadıkça korunmuyor.
> 4. **Beş aktivasyon hata ekranında CSP yok** (`Activation.render`) ve `Message`
>    metinlerini bu pakette hiçbir test pinlemiyor — M5-02'nin akışı, bilinçli
>    olarak kapsam dışı.
> 5. **Üretilen `*_templ.go`'nun elle düzenlenmesi** — `make test` onları yeniden
>    üretmiyor; `make check` yalnız diff'i yakalar, niyeti değil.
> 6. Runtime'da `<script>` ile yazılacak metin — bu ekranlarda script yok.
>
> 7. **YANIT BAŞLIKLARI** (10. turda eklendi). Hiçbir test gövde dışını okumuyor;
>    `Tap.render` dört başlık koyuyor ve beşincisinin **yokluğunu** kimse iddia
>    etmiyor. Ölçüldü: `w.Header().Set("Refresh", "30;url=https://evil.example/x")`
>    → `make test` **tamamen yeşil**, ve şablona hiç dokunmadan meta-refresh'in
>    etkisini üretiyor. Yalnız `Content-Security-Policy` pinli
>    (`TestTapResponses_CarryTheContentSecurityPolicy`). **Yani bu yüzeydeki URL
>    kanalı "meta refresh" değil; "meta refresh VE kimsenin izlemediği her
>    navigasyon başlığı".**
>
> 8. **Not sabitlerinin üretime bağlılığı** (13. tur). `result_test.go`'daki beş
>    not sabitinin **beşi de** artık `TestCheckinDB_ScreenTextIsWhatProductionRenders`
>    tarafından gerçek motordan sürülüyor. 13. tura kadar **dördü** sürülüyordu:
>    `noteStaleOpenIn` elle yazılmış bir kopyaydı ve motordaki metin değişse hiçbir
>    test kırılmıyordu (ölçüldü: `staleOpenInNote` yeniden yazıldı → **suite yeşil**).
>    Vaka 19 saatlik açık bir check-in tohumlanarak üretildi (eşik 18s); aynı
>    mutasyon şimdi **RED**.
>
> **5. `reject` başlığı: "Not recorded" → "Not counted"** (3. turda kırmızı çizgi
> denetiminde bulundu, **bloklayan**). Sayfanın en büyük yazısı kaydın **yokluğunu**
> beyan ediyordu, dört satır altındaki *"This tap was recorded but not counted"*
> ile çelişerek. Ölçüm: `checkin.Service.write` hata verirse `Record` hata döndürür
> ve `OutcomeRecorded` ancak INSERT geçtikten sonra kurulur — yani bu ekranın
> render edilmiş olması kaydın **var olduğunun kanıtı**. §5 satır 1/2/4'ün üçü de
> `reject`; §4.6'nın çalışana verdiği tek şey *itiraz edilebilir kayıt* ve başlık
> onu geri alıyordu. Ayrıca **bilinmeyen verdict** artık yönlü başlık almıyor
> (eskiden "Tapped in"e düşüyordu). Kusur M5-05'ten devralınmıştı ama M5-06 bu
> ekranın kopya geçişi görevi — dört verdict'in cümlesi yeniden türetilirken
> sayfadaki en büyük cümle atlanmıştı.
>
> **6. `flag` cümlesi onayı VAAT ETMİYOR ve itiraz kapısını kapatmıyor** (4. turda).
> Eski metin *"your manager **will** confirm this one … There is **nothing for you
> to do**"* diyordu: müdür bir flag'i **reddedebilir**, yani cümle kimsenin
> vermediği bir kararı beyan ediyordu; ve itiraz kapısını, tam da §4.6'nın onu
> korumak için var olduğu verdict'te kapatıyordu. Yeni metin: *"Recorded — it needs
> your manager's approval before it counts toward your hours. Tell them if that
> looks wrong."* Diğer üç dalın kaçış kapısıyla aynı hizada.
>
> **7. Damga kontrastı: 5. turda YORUM düzeltildi, kod 2026-08-01'de düzeldi.**
> *(Bu madde 2026-08-01'de güncellendi. Başlığı "yorum düzeltildi, kod DEĞİL"di ve
> artık doğru değil: kullanıcı kararı geldi ve kod değişti. Aşağıdaki 5. tur
> ölçümü tarihsel kayıt olarak duruyor; kararın kendisi maddenin sonunda.)*
>
> `stamp` bileşeninin
> yorumu *"the screen reads correctly **in monochrome** and to a screen reader"*
> diyordu; ekran-okuyucu yarısı doğru, **gören kullanıcı yarısı yanlış**: `.stamp`
> 11px bold + `opacity:.8`, ve `paper #FFFDF4` üstünde `stamp--ignored`
> (`line #C9D2C8`) **1,52:1**, `stamp--flagged` (`saffron #D98E2B`) **2,62:1** —
> AA 4,5:1 ister. Bu iki verdict'te gören kullanıcı damgadan ne rengi ne kelimeyi
> alıyor. Durumu onlar için taşıyan şey **kapanış cümlesi** — ve onun kontrastı
> **5,70:1**, 15. turda düzeltildi: `~16:1` **yanlıştı**. İki hata üst üste
> gelmişti: (i) kapanış paragrafları `text-ink/70` (`rgba(21,34,25,.7)`), düz `ink`
> değil; (ii) `closing()` **docket'in DIŞINDA** çalışıyor (`</section>`'dan sonra
> çağrılıyor), yani zemin `bg-paper #FFFDF4` değil gövdenin `bg-porcelain #EDF0EA`'sı.
> Tarayıcının yaptığı gibi sRGB'de harmanlanınca `rgb(86,96,88)` → **5,70:1**.
> **AA'yı geçiyor** (4,5:1), yani kod değişmedi — değişen **iddia**. `~16:1`
> **başlığın** değeri (docket içinde düz `text-ink` on `bg-paper` = 16,17:1), başka
> bir eleman başka bir zemin üstünde. Başlık dört verdict'in **ikisinde** yardım
> ediyor (*"Already tapped"*, *"Not counted"*); **`flag`'de etmiyor** — başlığı
> `ok` ile aynı, dolayısıyla **o gün** AA'yı geçen tek taşıyıcı kapanış cümlesiydi.
>
> **KARAR VE DÜZELTME (kullanıcı, 2026-08-01).** 5. turda kod değiştirilmemişti,
> çünkü `ignored → line` skill'in mandası ve bu bir **marka** kararı; kullanıcıya
> soruldu. Verilen karar: **damganın METNİ `ink` olur, durum RENGİ çerçevede
> kalır** (2px kenarlık + 1px iç halka + `bg-<token>/10` zemin). Gerekçe: durum →
> renk eşlemesi **korunur**, palete **yeni token girmez**, beş damga da okunur olur,
> kaşe hissi çerçeveden gelir. `input.css` buna göre yeniden yazıldı; `.templ`
> sınıf adları **değişmedi** (`stamp--approved` … `stamp--training`), yani M5-05'in
> "sınıf adı taranan `.templ`'de literal yaşamalı" kuralına dokunulmadı.
>
> `opacity-80` de **kaldırıldı**, ve gerekçesi AA değil: grup opaklığı içindeki her
> şeyi soluklaştırıyor, rengi taşıyan şey ise artık çerçeve — saffron kenarlığı
> paper üstünde 2,62:1 → **2,14:1**, `line` kenarlığı 1,52:1 → **1,39:1**. Kelime
> her iki hâlde de AA'yı geçiyordu (`ink@.8` on paper = **8,54:1**, ölçüldü; bu sayı
> önceden "≈11:1" diye tahmin edilmişti, ölçüm **8,54** dedi).
>
> Ölçülen kontrast (zemin `paper #FFFDF4`; damganın docket'in **içinde** render
> edildiği **üretilen HTML'den** doğrulandı, varsayılmadı):
>
> | Damga | ÖNCE (kelime = durum rengi) | ÖNCE @`.8` | SONRA (kelime `ink`, zemin `<token>/10`) |
> |---|---|---|---|
> | `approved` | 7,73:1 ✅ | 4,70:1 ✅ | **13,85:1** ✅ |
> | `flagged` | 2,62:1 ❌ | 2,14:1 ❌ | **14,81:1** ✅ |
> | `rejected` | 5,30:1 ✅ | 3,77:1 ❌ | **13,99:1** ✅ |
> | `ignored` | 1,52:1 ❌ | 1,39:1 ❌ | **15,55:1** ✅ |
> | `training` | 16,17:1 ✅ | 8,54:1 ✅ | **13,27:1** ✅ |
>
> Yani `flag`'de artık **iki** taşıyıcı var (damga 14,81:1 + kapanış cümlesi
> 5,70:1), bir değil.
>
> **Skill de düzeltildi.** `tappa-brand` hem "durum → renk eşlemesi sabittir" hem
> "Kontrast AA" diyordu ve ikisinin **çakıştığını hiç ölçmemişti**; beş damganın
> **gerçekte render edilen** hâlleri (`opacity:.8` uygulanmış) sayıldığında **üçü**
> AA'nın altındaydı (`ignored` 1,39 · `flagged` 2,14 · `rejected` 3,77; `approved`
> 4,70 ile sınırın hemen üstünde). Skill artık kelimenin `ink`
> olduğunu, rengin çerçeveye gittiğini, ölçülen sayıları ve **kendi kuralını
> çiğnediğini** açıkça yazıyor.
>
> **KONTRAST ARTIK BİR TESTLE KORUNUYOR — ama dar ve CI'da koşmuyor.** Mutasyonla
> ölçüldü: kelimeyi durum rengine geri döndürmek suite'i **tamamen yeşil**
> bırakıyordu — **1158 PASS / 0 FAIL**, mutasyon uygulanmış ve bu test **henüz
> yazılmamışken** ölçüldü — bu dosyadaki damga testlerinin hepsi HTML
> okuyor, HTML ise iki hâlde de aynı. `TestCompiledCSS_StampWordIsInk` eklendi:
> derlenmiş `app.css`'i okur ve iddiası şu — **seçicisinin METNİNDE `.stamp` GEÇEN**
> her kural, eğer `color:` bildiriyorsa `ink` bildirmek zorunda; artı beş
> modifier'ın **adının** derlenmiş CSS'te bulunması.
>
> ⚠️ **Testin İLK sürümü daha dardı ve denetim iki kaçış ölçtü.** Seçicileri
> *şekle* göre ayırıyordu (`== ".stamp"` ya da `.stamp--` öneki), ve ikisi de
> yeşil kalıyordu: `.stamp.stamp--flagged{color:…}` (**bileşik seçici**, ikisine de
> uymuyor) ve temel kuraldan sonra **ikinci bir** `.stamp{color:…}` (kontrol yalnız
> *bir* `.stamp` kuralının ink verdiğine bakıyordu). İkisi de artık reddediliyor.
>
> 🔴 **GERİ ÇEKİLEN CÜMLE (2. denetim turu).** Burada *"kaçacak şekil kalmıyor"* ve
> *"yanlış KIRMIZI üretir, yanlış yeşil değil"* yazıyordu. **İkisi de yanlıştı**;
> denetçi bu cümlelere karşı **üç yanlış yeşil** üretti ve üçü de bizzat yeniden
> ölçüldü (aşağıda, SINIR v). Testin gerçekte sorduğu şey daha dar: *"bir damgayla
> **eşleşebilen**"* değil, *"seçicisinin metninde `.stamp` **geçen**"*. Fazla-kapsama
> yalnız **öbür** yönde duruyor (`.stamp + p{color:…}` de reddedilir — o yanlış
> KIRMIZI'dır); geri çekilen, bu yönle ilgili olan iddia.
>
> ⚠️ **`modifiersSeen` nöbetçisi de saydığını sanmıyordu.** *Occurrence* sayıyordu:
> taze build'de dokuz `.stamp--*` seçici var, eşik beşti, yani **dört slack**. İki
> modifier kuralı tamamen silinip sayı **5**'e düştüğünde test **yeşil** kalıyordu
> (ölçüldü) — iki damga çıplak render edilirken. Artık **distinct ad** aranıyor:
> `approved · flagged · rejected · ignored · training`.
>
> **Dört mutasyonla kanıtlandı, dördü de RED:** (a) kelime geri durum renginde ·
> (b) `.stamp`'ten `text-ink` silinince · (c) `.stamp.stamp--flagged` bileşik
> seçicisi · (d) ikinci `.stamp{color:…}` kuralı. Modifier nöbetçisi ayrıca (e) iki
> modifier kuralı silinince **RED** (mesaj eksik adı yazıyor).
> **SINIRLARI:** (i) `TestCompiledCSS_GeneratesNoText` gibi **CI'DA DAİMA SKIP** —
> `ci.yml` CSS derlemiyor, `.gitignore:16` `app.css`'i checkout dışında tutuyor, ve
> **atlanması geçme değildir**; (ii) **bildirilen** rengi okur, render edilen
> pikseli değil — ata bir `opacity`/`filter` ya da paper'dan koyu bir zemin bu
> testin göremeyeceği şeyler; (iii) Tailwind'in `rgb(21 34 25/…)` yazımına bağlı;
> (iv) 🔴 **BİR KURAL DAMGANIN RENGİNİ BU TARAMA GÖRMEDEN DEĞİŞTİREBİLİR.** Tarama
> seçicideki `.stamp` **metnine** bakıyor ve yalnız `color:` bildirimlerini okuyor.
> **Dördü** ölçüldü, **dördü de suite'i tamamen yeşil bıraktı** (sıfır FAIL
> satırı; sayı burada rakamla yazılıyor çünkü `go test`in FAIL satır kalıbını
> düzyazıya yazmak, bu dosyanın diff'ini okuyan bir log sayımını yanıltıyor —
> ölçüldü, `make check` çıktısında tam olarak bu oldu):
> **(1)** `class="stamp stamp--training text-saffron"` — **markup'ta sıradan bir
> utility**, yani bu repoda renk eklemenin normal yolu; `.text-saffron` utilities
> katmanında, eşit özgüllük, dosyada sonra → kazanıyor, kelime **2,15:1**.
> **(2)** `.docket * { color: … }` — eşit özgüllük, sonra geliyor.
> **(3)** `span[class~="stamp"] { color: … }` — **daha yüksek** özgüllük, üstelik
> minifier tırnakları atıyor (`span[class~=stamp]`) yani seçici `.stamp` metnini
> **hiç içermiyor**.
> **(4)** `.stamp--flagged { @apply text-opacity-10; }` — bu sınıfı **adıyla anıyor**
> ve yine de geçiyor: derlenen hâli `.stamp--flagged{--tw-text-opacity:0.1}`
> (**bayt 7785**, temel `.stamp` kuralı **5956**'da, yani sonra ve eşit özgüllük) ve
> **hiçbir `color:` bildirimini değiştirmiyor**. Temel kuralın dizesi hâlâ
> `color:rgb(21 34 25/var(--tw-text-opacity,1))`, tarama ink okuyup geçiyor; FLAGGED
> **1,22:1** render ediliyor — **bu görevin başladığı 2,62'den bile kötü**. Bu madde
> (ii)'ye de ait: **beyan edilen** renk ink, **render edilen** piksel değil.
> **(1) KAPANDI** — ama bu testte değil, **öbür yarıda**:
> `TestResultScreen_PracticeIsStampedAndSaysItDoesNotCount` artık TRAINING'in tam
> `class` dizesini pinliyor (dört verdict damgası zaten öyleydi; TRAINING yalnız
> `Contains("stamp--training")` ile tutuluyordu ve kaçış tam oradan girdi).
> **(2), (3) ve (4) AÇIK**: kapatmak keyfî seçiciler üzerinde cascade çözmeyi **ve**
> Tailwind'in kendi değişkenlerini yerine koymayı gerektirir, o da bir tarayıcı —
> **garanti değil, liste olarak** taşınıyor;
> (v) **stylesheet'i okur, şablonun o sınıfı hâlâ KULLANIP kullanmadığını göremez.**
> Ölçüldü: `result.templ`'in markup'ından iki damga sınıfı silindiğinde beş
> modifier'ın **beşi de** `app.css`'te kaldı (adlar yorumlarda geçtiği için) ve bu
> test **yeşil**; onu yakalayan
> `TestResultScreen_StampCarriesTheWordAndTheBrandClass` (aynı mutasyon → **4 FAIL
> satırı**). İki test bir çiftin iki yarısı.
> `.stamp`'in `opacity`'si **bilinçli olarak pinlenmedi**: kaldırılması çerçeve
> kararıydı ve bir marka tercihini teste çivilemek bu testin işi değil.
> ⚠️ **Bunun eski gerekçesi fazla dardı:** *"ink@.8 = 8,54:1, AA'yı geçiyor"*
> deniyordu — `.8` için doğru, **başka hiçbir değer için bir şey söylemiyor**;
> `.1`'de aynı aritmetik **1,22:1** veriyor (yukarıda kaçış 4, ölçüldü). Dürüst
> ifade *"opacity burada güvenli"* değil, **"bu dosyada opacity'yi kısıtlayan hiçbir
> şey yok"**tur; ortaya çıkan kontrastı `input.css`'teki ölçümler ve insan denetimi
> taşıyor.
>
> **Kapatılmayan sınır:** çerçevenin kendisi WCAG 1.4.11'in metin-dışı **3:1**'ini
> saffron (2,62) ve line (1,52) için geçmiyor. Durumu **kelime** taşıdığı için renk
> pekiştirme sayılıyor ve 1.4.11 bunu zorunlu tutmuyor — kapatılmadı, **sınır olarak
> yazıldı**.
>
> **Ortak `Problem` şablonu ONBİR ekrana düşüyor**, dokuza değil (5 aktivasyon
> sabiti `activate.go:161-185` + 6 tap sabiti `tap.go`). Pinlenen altısı tap
> ailesi; **beş aktivasyon sabitinin metni bilinçli olarak pinlenmedi** —
> onlar M5-02'nin kopyası, o akış dört denetim turu gördü, ve buradan yazılacak
> bir test o metnin sahipliğini sessizce devralırdı. **Açık boşluk:**
> `problemServer.Message` (*"Nothing was activated and nothing was recorded
> against you"*) bu pakette hiçbir test tarafından korunmuyor.
>
> **Ölçülemeyen/olmayan:** bu ekranda **süre yok** (vardiya toplamı rapor işi,
> M6), dolayısıyla "saat ve süre mono" kriteri var olan tek yarısıyla — duvar
> saati + trust puanı, ikisi de `font-mono` — karşılanıyor.

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

> **Kart düzeltmesi (2026-08-01, M5-07 uygulaması sırasında).** Dört kriterden
> **ikisi zaten sağlanıyordu** (ölçüldü, aşağıda), biri kartın yazdığından **dar**
> çıktı, ve üçüncü slaytın metni §4.6 gereği **yeniden yazıldı**.
>
> **1. "İlk tap practice + TRAINING damgası" ZATEN sağlanıyordu** (M4-06 + M5-06).
> `practice` sunucu türetimli (`isPracticeTap`: `Employee.ActivatedAt` dolu **ve**
> `LastForPerson == nil` **ve** `LastOpenIn == nil`), `Input`'ta istemci alanı yok
> (`TestInput_HasNoClientPracticeField` — alan eklendi, test **RED**), damga ve
> "does not count toward your hours" cümlesi `result.templ`'de pinli. M5-07'nin
> eklediği: aynı iddianın **HTTP sınırından** denenmesi
> (`TestTourDB_AClientCannotDeclareOrRefuseThePracticeFlag` — `practice`,
> `Practice`, `is_practice`, `training`, `practice_tap` alanlarıyla hem **talep**
> hem **ret** denendi, ikisi de kolonu değiştirmedi; handler yalnız `ctx`,
> `occurred_at`, `lat`, `lng` okuyor).
>
> **2. "Çalışılan saate asla sayılmıyor" — BUGÜN ÖLÇÜLEBİLİR HÂLİ DAHA DAR.**
> Repoda **hiçbir saat toplamı yok** (ölçüldü: `worked_hours|WorkedHours|SumHours|
> total_hours` → 0 eşleşme; raporlar M6). Bugün gerçekten uygulanan iki şey var:
> (a) practice kaydı **yön zincirini açık tutmuyor**, yani hiçbir in/out çiftine
> giremez; (b) `transactions.practice` değişmez satırda duruyor, yani gelecekteki
> rapor onu dışlayabilir. Kartın cümlesi doğru ama **gelecek zamanlı**; bugün
> sınanabilen kısım (a)'dır ve iki kere sınanıyor.
>
> **🔴 VE ORADA GERÇEK BİR BOŞLUK VARDI.** Yön zinciri koruması **iki yerde**:
> `checkin.go` `gather` (`if !open.Practice`) ve `tap/decide.go` `resolveDirection`
> (`open == nil || open.Practice`). İkisi **aynı gözlemlenebilir sonucu** ürettiği
> için **birini tek başına silmek tüm suite'i yeşil bırakıyordu** — ölçüldü:
> `checkin.go` guard'ı silindi + `gather_practice_db_test.go` geri çekildi →
> **`make test`** (yani `.env` yüklü, gerçek Postgres) → **13 ok / 0 FAIL**.
> *(Bu satır ilk yazılışında çıplak `go test ./...` diye alıntılanmıştı — tam da
> `agent-brief.md`'nin "her DB testini sessizce SKIP eder" diye işaretlediği komut.
> Denetimde yakalandı, `make test` ile yeniden ölçüldü, sayı değişmedi; ve
> `TestCheckinDB_PracticeThenDirection…` ile `TestTourDB_Skipping…`'in gerçekten
> PASS olduğu — SKIP değil — ayrıca doğrulandı.)* (Öbür yön
> zaten kırmızıydı: `decide.go` guard'ı silinince
> `TestDecide_DirectionPracticeOpenInDoesNotCloseChain` **RED**.) Yani "iki koruma"
> bir tanesinin sessizce ölmesine açıktı. Kapatıldı:
> `TestGatherDB_APracticeCheckInIsNeverHandedOnAsAnOpenOne` `gather`'ı gerçek
> Postgres'e karşı kendi sınırında sürüyor (pozitif kontrollü: practice=false satır
> **taşınıyor**, practice=true satır **taşınmıyor**); aynı mutasyon artık **RED**.
>
> **Ve altında ÜÇÜNCÜ bir eşdeğer mutant vardı** (denetimde bulundu):
> `checkin.go` `transaction()` içindeki `Practice: t.Practice` → `false` de suite'i
> **yeşil** bırakıyordu. Eşdeğerdi, çünkü `tap.Transaction.Practice`'in tek
> tüketicisi `resolveDirection` ve `gather` filtresi ayaktayken o alan üretimde hiç
> `true` olmuyor. Yani ortada *"iki canlı koruma"* değil, **bir canlı guard + beslemesi
> pinlenmemiş bir yedek** vardı: `gather` filtresini *"motor zaten koruyor"* diye
> silen biri, sabit `false` ile beslenen bir guard'a güvenmiş olurdu.
> `TestTransaction_CarriesThePracticeColumn` beslemeyi pinliyor; aynı mutasyon
> artık **RED**. Üçü birlikte: her katman kendi sınırında ölçülü.
>
> **3. Tur SUNUCU TARAFINDA üç GET'tir; JS yok, istemci state'i yok, YAZMA yok.**
> `GET /activate/tour?step=1..3`, ilerleme bir **link**. Adım dışı/bozuk değer
> **1'e kırpılır** (hata ekranı değil). Tur `transactions`, `audit_log` veya çerez
> **yazmaz** — ölçüldü (`TestTourDB_WritesNothing`: 7 istek boyunca iki sayaç da
> sabit, ardından **pozitif kontrol** olarak gerçek bir tap sayacı +1 yapıyor).
> "Atlanabilir" bunun sonucudur: atlamak da bitirmek de aynı linki izlemektir.
> Turun view modeli **tek alan** taşır (`TourView.Step`) ve **hiçbir kişisel veri**
> taşımaz — isim, mekân, id, kod hiçbiri (`TestTour_CarriesNoPersonalDataAtAll`,
> `TestTourView_FieldCountIsTheSpec`).
>
> **4. 🔴 ÜÇÜNCÜ SLAYT "SONRAKİ TAP'İN PRACTICE OLACAĞINI" SÖYLEMİYOR — ölçüm
> öyle demiyor.** `GetLastTransactionForEmployee` **verdict ve kanal yüklemi
> taşımıyor** ve §4.6 her kararlı tap'i yazıyor, dolayısıyla practice hakkı
> **herhangi bir önceki kayıtla** harcanır. Ölçüldü
> (`TestDecide_ThePracticeRunIsSpentByANYPriorRecord`):
>
> | Önceki kayıt | Sonraki tap practice mi |
> |---|---|
> | hiç yok | **evet** |
> | `reject` (emekli plaket · geçersiz SUN · deaktif çalışan) | **hayır** |
> | `ignored` (debounce) | **hayır** |
> | manuel satır (M6-04, bugün üretilemiyor) | **hayır** |
>
> Ayrıca ölçülenler: **ilk tap asla `ignored` olamaz** (debounce'un ölçeceği önceki
> tap yok → `TestDecide_TheFirstTapCanNeverBeIgnored`); **QR ilk tap'i de practice**
> (kanal okunmuyor; hem `ok` hem `flag` dalında); **practice + `flag` üretilebilir**
> ve `result.templ` o bileşimde "before it counts" demiyor.
>
> **En zararlı vaka İKİNCİ CİHAZ.** Zaten tap etmiş biri yeni telefon kurduğunda
> sonraki tap'i practice **değildir**; ona "ilk tap'in deneme" demek, gerçek bir
> giriş kaydını deneme sanmasına ve günü yanlış kapatmasına yol açardı. Çözüm
> `Submit`'te: **ilk aktivasyon → `/activate/tour`**, **ikinci cihaz →
> `/activate/done?replaced=1`** (bugünkü davranışın aynısı). Gerekçe ölçülebilir:
> `SecondDeviceReplaced` tam olarak `employees.status` zaten `'active'` iken
> doğrudur, ve hiç aktive olmamış biri hiç oturum tutmamıştır → hiç tap etmemiştir.
> Uçtan uca kanıt: `TestTourDB_ASecondDeviceIsNotShownThePractisePromise`.
> Slaytın metni yine de **"ilk tap"** hakkında konuşuyor, "sıradaki tap" hakkında
> değil — turu elle açan ikinci cihaz kullanıcısı için de doğru kalsın diye.
>
> **5. Kapsam kararı: tur M5-06'nın ÜÇ beyaz listesine EKLENDİ** (kartın dışında
> bırakılmadı), **ve bir DÖRDÜNCÜ liste yazılması gerekti.** M5-06 o mekanizmanın
> sınırını *"kapsam ekran başına ve elle"* diye yazmıştı; tur o günden beri eklenen
> ilk ekran, ve liste genişletildi: metin (`TestTour_SaysExactlyThisAndNothingElse`,
> `screenText` ile tüm belge), eleman (`TestTour_RendersOnlyTheseElements`,
> **13** etiketlik kapalı küme) ve referans (`TestTour_PointsOnlyAtItsOwnFlow`,
> adım başına birebir href **değer** kümesi). Mutasyonla: eklenen cümle **RED**,
> eklenen `<iframe srcdoc>` **RED** (metin testi görmüyor — `srcdoc` `textAttrRE`'de
> yok, kapalı küme yakalıyor), `https://evil.example/x` **RED**.
>
> **Ve `ping` de açıktaydı** (ikinci denetimde bulundu, bloklamayan).
> `<a href="/activate/done" ping="https://evil.example/beacon">` üretilen slayta
> ulaşıyor ve **tüm handler paketi yeşil** kalıyordu: `assertRefs` yalnız `href`,
> `src` ve `meta http-equiv` okuyordu, `ping` ise hiçbir şeyin *fetch etmediği* ama
> tarayıcının tıklamada **POST attığı** bir öznitelik — yani markanın *"mutlak URL
> sayısı 0"* kuralının tam ihlali. `refRE`'ye `ping` eklendi; repoda hiçbir şablonda
> `ping` geçmediği için mevcut hiçbir beklenti kımıldamadı (ölçüldü). Mutasyon iki
> yerde **RED**: tur slaytında ve **ortak `Problem` şablonunda** (on bir ekran).
> `TestTour_PointsOnlyAtItsOwnFlow`'un *"her slaytın yaydığı adres kümesini
> pinler"* cümlesi de **öznitelik adlarını sayacak** şekilde indirildi — liste hâlâ
> bir liste, ve bunu artık yorumun kendisi söylüyor.
>
> **🔴 ÜÇÜ BİRDEN YETMEDİ — DOKUNMA HEDEFİ SAYISI AÇIKTA KALMIŞTI** (denetimde
> bulundu, **bloklayan**). `assertRefs` href **değerlerini** karşılaştırıyor,
> **kaçını** değil. Slayt 2'ye eklenen
> `<a href="/activate/done" class="tap-button …"></a>` — **boş etiketli** ikinci bir
> dokunma hedefi — tüm paketi **yeşil** bıraktı: metin pini metin düğümü görmüyor,
> eleman kümesi `a`'ya zaten izin veriyor, referans kümesinde o adres zaten var.
> §9'un *"bu ekranlarda ikinci aksiyon olmaz"* kuralı tam olarak buna bakıyordu.
> `TestTour_HasExactlyTheseTouchTargets` eklendi: adım başına **sıralı
> (hedef → etiket)** listesi + izinsiz `on…=` özniteliği reddi. Beş mutasyon,
> beşi **RED**: boş anchor · etiketli fazladan anchor · silinen skip linki
> (**4 test** kırılıyor) · yeniden adlandırılan anchor · izinli elemana konan
> `onclick`.
> *(Aynı yorumda ikinci bir hata daha vardı: "üç slayt, iki link" — slayt 3'te bir
> link var, toplam **beş** anchor. Cümle geri çekildi ve düzeltildi.)*
>
> **SINIRLAR — garanti değil, sayıldı:**
> (i) tur `Activation.render`'dan geçtiği için **CSP taşımıyor**, ve bu boşluk
> M5-06'nın yazdığından **geniş**: `Activation.render` **hiçbir** yanıta CSP
> koymuyor (ölçüldü: fonksiyon gövdesinde `Content-Security-Policy` sayısı **0**),
> yani beş hata ekranı **değil**, `Activate` · `Confirm` · `Done` · **`Tour`** +
> beş `problem*` sabiti = **dokuz** ekran. Karşılaştırma: `Tap.render` yazdığı
> **her** yanıta koyuyor.
> (ii) dokunma hedefleri: `tap-button` 64px (`min-h-16`), skip linki 44px
> (`min-h-11` → derlenmiş CSS'te `min-height:2.75rem`) — ama **hiçbir test piksel
> ölçmüyor**, ve `TestTour_HasExactlyTheseTouchTargets` **markup** okuyor: yalnız
> stille basılabilir hâle getirilmiş bir alanı (dev bir `::after`) göremez.
> (iii) `screenText`'in bilinen açık kanalları (CSS `content:`, `meta description`,
> yanıt başlıkları) turda da açık — tur onları kapatmıyor, aynı listelere giriyor.
> (iv) slayt 1 *"telefonun bu sayfayı açar"* diyor, oysa iPhone X ve öncesi arka
> planda NFC okuyamaz (M5-08) — bu yüzden slayt *"telefonun tepki vermezse
> müdürüne söyle"* ile bitiyor; QR'dan söz **etmiyor**, çünkü kanal henüz yok.
> (v) **slayt 3'ün ilk cümlesi koşulsuz:** *"your first tap is a practice run."*
> İlk tap `reject` olursa o tap ne practice olur ne TRAINING damgası alır. Hata
> yönü **güvenli** (reject de saate girmez, yani kimseye "sayılmadı" denip
> sayılmıyor) ve slayt *"Whatever a tap turns out to be, the screen right after it
> says so"* ile kapanıyor — yani çalışan her hâlükârda doğru bilgiyi onay
> ekranından alıyor. Koşullandırmak render anında bir DB okuması gerektirirdi;
> **sınır olarak yazıldı**, kapatılmadı.
> (vi) **Oran sınırı:** tur, bir aktivasyonun IP başına harcadığı istek sayısını
> **4 → 5** (atlayan) / **4 → 7** (tam tur) yapıyor — ölçüldü, sayılarak değil,
> istekleri sayan bir sarmalayıcıyla. `floodLimit=600`/10 dk demek: aynı IP
> anahtarından 10 dakikada **150 → 120 / 85** tam aktivasyon. Gerçek onboarding
> hızının çok üstünde (bir mekân bir seferde onlarca çalışan aktive eder), ama
> **tek NAT arkasındaki tavan bu kadar düştü** ve yazılı olması gerekiyordu.
> `ratelimit.go`'nun yorumu da düzeltildi: *"onbeş kişi ≈ **45** istek"* diyordu ve
> **turdan önce de yanlıştı** (üç istek/aktivasyon varsayıyor, kod taşıyan URL'den
> temiz URL'e giden 303 hop'unu hiç saymıyor; ölçülen HEAD değeri **60**). Artık
> tek sayı değil, üç şeklin tablosu yazılı: **4 / 5 / 7** istek → onbeş kişi için
> **60 / 75 / 105**. Tur atlanabilir olduğu için tek bir sayı zaten yanlış olurdu.
>
> **İleri kartlar da kapatıldı** (denetim bulgusu, kod değişikliği yok):
> [m6-dashboard.md](m6-dashboard.md) M6-07 *"açık kalan giriş"* ve M6-11
> *"çıkışsız açık kayıtlar"* kriterleri practice istisnasını **saymıyordu**, oysa
> aynı kartın **saat** kriteri sayıyor. Practice bir `type='in'` satırıdır ve
> ardından hiç `out` gelmez → SQL anlamında sonsuza dek "açık" görünür. Bugünkü
> etkisi sıfır (rapor yok), ama M6'da **her yeni çalışanın deneme tap'i müdürün
> "eylem gerekiyor" kuyruğunda** belirirdi — yani turun *"deneme"* vaadi anomali
> olarak okunurdu. İki kritere de tarihli not eklendi.
>
> **Tailwind:** yeni tek seçici `.min-h-11` (+29 bayt, 14.283 → 14.312) ve o sınıf
> gerçek bir `class` özniteliğinde geçiyor (`activate.templ`, `tourSkip`).
> Düzyazıdan doğan **sıfır** yeni ölü kural (taze build'in seçici kümesi HEAD'inkiyle
> bu tek satır dışında birebir).
>
> **Kapsam dışı, tek satır, bilinçli:** `internal/policy/document.go`'da
> `EffectIgnore` yorumu *"debounce — no record"* diyordu ve bu **yanlış**;
> `checkin.go` yalnız `EffectRedirect`'te yazmayı atlıyor, `ignored` **satır
> yazıyor** (gerçek Postgres'te ölçülü, `checkin_db_test.go` §5 satır 5). Bu diff'e
> ait değildi ama §4.6 hakkında çelişik bir cümleydi; düzeltildi.

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
- **QR aynı kanıtla asla NFC'nin kanıt tavanına çıkmıyor:** yalnız GPS ile bir QR
  tap'i **hiçbir zaman insansız `ok` olamaz** (`base:qr-requires-ip` → `flag`),
  bir NFC tap'i olabilir (`base:gps-only-allow` → `ok`).
  ⚠️ Bu kriter **2026-08-01'de yeniden yazıldı**; eski hâli *"QR asla NFC ile aynı
  **trust** seviyesine çıkmıyor"*du ve **lafzı yanlıştı** — `trust` SAYISI kanalı
  hiç ayırmıyor (IP+GPS'li QR **100**, IP+GPS'li NFC **100**). Ölçüm, gerekçe ve
  kullanıcı kararı: aşağıdaki kart düzeltmesi md. 3.
- Plaket baskısında NFC + QR birlikte — tasarım notu `tappa-brand` ile uyumlu.
- **QR bazı çalışanlar için kalıcı yoldur, yedek değil.** iPhone X ve öncesi
  arka planda NFC etiketi okuyamaz (arka plan okuma iPhone XS/XR ve sonrası) —
  o telefonlardaki çalışanlar için URL hiç açılmaz. Q15 gereği QR'da IP zorunlu
  olduğundan, bu çalışanlar mekânın WiFi'ına bağlanmazsa **her gün** `flag`
  üretir. Pilot öncesi telefon envanteri çıkarılır ([M8-07](m8-deploy-pilot.md));
  oran yüksekse Q15 varsayılanı tenant bazında yeniden değerlendirilir.

> **Kart düzeltmesi (2026-08-01, M5-08 uygulaması sırasında).** Hiçbir kriter
> düşürülmedi. Kartın **iki cümlesi** ölçümle karşılaştırıldı (biri açık bir
> çelişki çıktı, karara bağlanmadı), bir **maskelenmiş mutant** bulunup kapatıldı
> ve M5-10'a sayıyla bir devir yazıldı.
>
> **1) Görevin çoğu HAZIRDI — ama YOLUN KENDİSİ hazır değildi, ve ilk iş bunu
> ölçmekti.** Hazır olanlar tek tek doğrulandı: `sun.Parse` `ctr` ve `cmac`'in
> **ikisi de yokken** `Channel=qr` üretiyor · `preview.go` adım 4'te QR'ı kriptoya
> dokunmadan kısa devre yapıyor · `base:qr-requires-ip` baseline'da · `decide_test.go`
> ve `policyset_db_test.go`'da QR vakaları var · bağlam `Channel`'ı sunucudan
> türetilmiş taşıyor.
> **Ama boşluk tam ortadaydı:** `grep` ile ölçüldü, bu pakette **hiçbir** `GET /t`
> isteği `&ctr=&cmac=` **olmadan** yapılmıyordu; QR'lı her DB testi bağlamı
> **elle** kuruyordu (`h.qrContext()`). Yani *"SUN parametresiz bir plaket URL'i →
> kaydedilmiş QR tap'i"* zincirinin **hiçbir testi yoktu** — M5-05 dersinin birebir
> şekli (*"A'da kanıtlanan garanti B'de tüketiliyorsa B'nin onu KULLANDIĞI ayrıca
> pinlenmeli"*). Yeni dosya `internal/handler/qr_db_test.go` **her testte
> `GET /t?tag=<uid>` ile başlıyor ve sayfanın KENDİ mint ettiği bağlamı** POST
> ediyor.
>
> **2) Beş kriterin durumu (ölçüm, iddia değil).**
>
> | Kriter | Önceden | M5-08'in eklediği |
> |---|---|---|
> | `sun_valid=false`, `channel='qr'` | sütun doğru yazılıyordu (elle kurulmuş bağlamla) | **uçtan uca**: GET → mint → POST → sütun; ayrıca `ctr` **NULL** ve `tags.last_ctr` **kıpırdamıyor** |
> | IP zorunlu, GPS tek başına yetmez | `decide_test.go` (saf) + policy testleri | **aynı kanıtla iki kanal yan yana**: QR+GPS → `flag`/`base:qr-requires-ip`, NFC+GPS → `ok`/`base:gps-only-allow` |
> | QR ≠ NFC trust seviyesi | — | **ölçüldü, çelişki çıktı** (md. 3) |
> | Plaket baskısı NFC+QR | yoktu | skill **`tappa-brand` → "Plaket baskısı"** bölümü (md. 5) |
> | QR kalıcı yol, "yedek" değil | — | **yokluk ölçüldü** + tripwire testi (md. 6) |
>
> **3) 🔴 KARTIN TRUST CÜMLESİ YANLIŞTI — ÖLÇÜLDÜ, KULLANICI KARARI ALINDI
> (2026-08-01), KRİTER YENİDEN YAZILDI.**
>
> Kartın eski cümlesi *"QR asla NFC ile aynı **trust** seviyesine çıkmıyor"*du.
> `trustScore` (`decide.go:467`) ise **kanal terimi taşımıyor** ve bu bir kaza
> değil: formül **CLAUDE.md §5'te normatif** yazılı — `20 (taban) + 50 (IP eşleşti)
> + 30 (GPS eşleşti)` — ve trust **kanıtı** ölçer, **sonucu** değil (M4-06'nın
> kendi tuzağı). İki okuma da uçtan uca ölçüldü
> (`TestQRDB_WithoutAnIPMatchGPSAloneIsNotEnough`, üç alt vaka, gerçek Postgres):
>
> | Okuma | Ölçüm | Sonuç |
> |---|---|---|
> | *"trust" = SAYI* | IP+GPS: QR **100**, NFC **100** · yalnız GPS: QR **50**, NFC **50** | **Kartın LAFZI YANLIŞ.** Sayı kanalları hiç ayırmıyor. |
> | *"trust seviyesi" = KANIT TAVANI* | yalnız GPS: QR → **`flag`** (`base:qr-requires-ip`), NFC → **`ok`** (`base:gps-only-allow`) | **Kartın KASTI DOĞRU.** QR aynı kanıtla **asla insansız `ok` olamıyor**. |
>
> **KULLANICI KARARI (2026-08-01):**
> 1. **§5'in formülü DEĞİŞMİYOR.** Normatiftir, M4-06 testleri 20/50/70/100'ü
>    pinliyor, ve kanal terimi eklemek ADR + o testlerin güncellenmesi demekti —
>    ölçülen fayda bunu karşılamıyor, çünkü ayrımı zaten `transactions.channel`
>    taşıyor.
> 2. **Kartın cümlesi "kanıt tavanı" okumasına indirildi** (yukarıdaki kriter
>    listesinde yeniden yazıldı). Artık ölçülen şeyi söylüyor: *aynı kanıtla QR
>    asla insansız `ok` olamaz*.
> 3. **🔴 M6'YA DEVİR — TRUST'IN GÖRÜNDÜĞÜ HER YÜZEYDE `channel` DE GÖRÜNMELİ.**
>    Bugünkü sınır ölçüldü: `transactions.channel` **yazılıyor ama hiçbir
>    kullanıcı yüzeyinde gösterilmiyor** — `pages.ResultView`'da kanal alanı yok
>    (§9 gereği çalışan ekranına eklenmez) ve müdür arayüzü henüz yok. Sonuç: bir
>    müdür `Trust 100` görüp NFC sanar, oysa satır QR olabilir. **M6-01 (işlem
>    listesi) ve M6-04 (onay kuyruğu)** kanalı trust'ın yanında göstermek
>    zorundadır; aksi hâlde Q15'in ayırdığı iki dünya raporda tek görünür.
>    *(Orkestratör bunu `state.md`'ye devir olarak yazıyor.)*
>
> **4) 🔴 M5-10'A DEVİR — SAYIYLA. "Bir sonraki işlem yeni fiziksel dokunuş
> gerektirir" QR'da TUTMUYOR.**
> Ölçülen (12 POST, tek taranmış QR bağlamı, gerçek Postgres, `qr_db_test.go`):
> **12 istek → 12 `transactions` satırı**, hepsi 200; **1'i `ok`, 11'i `ignored`**;
> `tags.last_ctr` **kıpırdamadı**. **Kontrol (aynı döngü, NFC):** 12 istek → **1
> `ok` + 11 `reject`/`sys:sun-invalid`**, `last_ctr` **tam 1** ilerledi (700→701).
> Fark **tür farkı**: NFC'de sayaç harcanıyor, QR'da harcanacak sayaç yok.
>
> 🔴 **GERİ ÇEKİLEN SAYI (1. denetim turu, 2026-08-01).** Bu madde önce şöyle
> diyordu: *"`tapContextTTL = 900 sn` / `DebounceWindow = 60 sn` ⇒ tek taramadan
> **en fazla 16** yön taşıyan satır"*, ve *"M5-10 varsayılanda tavanı 16 → 4'e
> indirir"*. **İkisi de yanlıştı**, ve yanlışlık bir varsayımdaydı: aritmetik,
> debounce'un tekrarları **duvar saatinde** aralıklandırdığını varsayıyordu.
> **Aralıklandırmıyor.** `decide.go:181`:
> ```go
> gap := in.Now.Sub(in.LastForPerson.OccurredAt).Seconds()
> ```
> `in.Now` **sunucu** saati, ama `LastForPerson.OccurredAt` **istemcinin beyan
> ettiği** `occurred_at` (`handler/checkin.go` → `domain/checkin.Record`). Geçmiş
> bir `occurred_at` gap'i şişiriyor ve **`sys:person-debounce` hiç ateşlenmiyor**.
>
> **Yeniden üretilen ölçüm** (tek taranmış QR bağlamı, sabit ve 10 dk geçmiş bir
> `occurred_at`, `TestQRDB_ABackdatedOccurredAtNoLongerDefeatsTheDebounce`):
> **20 istek → 20 satır, 20'si de yön taşıyor** (`{in:1, out:19}`), **0 `ignored`**,
> hepsi `flag` (`base:queued-window`), duvar saati **0,51 sn**, `last_ctr`
> **700→700**. Yani 16 aritmetiği hem sayıca hem zamanca yanlıştı: 20 satır, 900
> saniyede değil **yarım saniyede**.
> **Kontrol — aynı numara NFC'de İŞE YARAMIYOR** (aynı testin ikinci yarısı):
> 20 istek → **1 `in`** + **19 `reject`/`sys:sun-invalid`**, `last_ctr`
> **700→701**. Freni sayaç tutuyor, debounce değil.
>
> **GERÇEK BAĞLAYICI: `tapSessionLimit`, ve o bir RATE DEĞİL BURST.**
> `httpx.Limiter` **sabit pencere** (`ratelimit.go`), yani bütçe anında
> harcanabiliyor. Monte edilmiş router üzerinden ölçüldü
> (`TestQRDB_TheExposureCeilingIsTheSessionBudget`, `GET /t` ile — o yol
> `transactions` yazmıyor): **300 istek arka arkaya servis edildi (~1,2 sn),
> 301.'si 429**. Dolayısıyla 15 dk'lık TTL içine **iki** 10 dk'lık pencere sığıyor
> ⇒ yapısal tavan **~600 istek**, eksi bağlamı basan 1 `GET` ⇒ **~599 yön taşıyan
> satır** — geri çekilen 16 sayısının **~37 katı**.
> **VE BU YÜZDEN M5-10 TAVANI 4'E İNDİRMİYOR.** 3 dk'lık tazelik penceresi hâlâ
> tam bir **300'lük burst**'e izin veriyor; yalnız **ikinci pencereyi** siliyor,
> yani tavan kabaca **yarılanıyor (~600 → ~300)**. M5-10'un kabul kriterine
> yazılacak doğru cümle: *"pencere QR'da da geçerlidir ve tavanı kabaca yarıya
> indirir; debounce bu kanalda hiçbir şeyi sınırlamıyor."*
>
> 🟢 **VE KURAL 2026-08-01'DE UYGULANDI — bu maddenin tavanı artık TARİHTİR.**
> Kullanıcı kararı: debounce iki saate birden bakar —
> `gap = min(now − LastForPerson.OccurredAt, SecondsSinceLastRecordedTap)`, manuel
> öncül sunucu bacağından muaf. ⚠️ İkinci terim bir **çıkarma değil**, SQL'in
> hesapladığı hazır bir **yaş**tır (`EXTRACT(EPOCH FROM (clock_timestamp() −
> created_at))`); Go'nun bir DB timestamp'ini kendi saatinden çıkardığı ilk yazım
> 4. katmanda ölçümle çöktü. Gerekçe, ölçümler, reddedilen alternatifler ve M9-01 çarpışması:
> **[ADR 0006](../adr/0006-debounce-iki-kosullu-zaman.md)**.
> **Kural sonrası yeniden ölçüm, aynı sonda (20 POST, tek taranmış QR bağlamı,
> sabit geçmiş `occurred_at`):** **20 satır** (§4.6 korunuyor — hiçbiri
> reddedilmiyor), ama **1 yön taşıyan + 19 `ignored`**. Yani *"tek taramadan kaç
> yön taşıyan satır"* sorusunun cevabı — **ardışık** trafik için — **~599 → 1**.
> ⚠️ **EŞZAMANLI trafik ayrı bir hikâyeydi ve o da kapatıldı:** `gather` ile
> `write` ayrı transaction'lar olduğu için 50 eşzamanlı POST **51 sayılan** satır
> üretiyordu (2 eşzamanlı POST bile yetiyordu). Kişi başına
> `pg_advisory_xact_lock` ile artık **50 satır, 1 sayılan, 49 `ignored`**;
> farklı kişiler birbirini beklemiyor (30 kişi eşzamanlı, 0 non-200).
> ⚠️ **VE İLK UYGULAMA YETMEDİ — denetim iki katman daha ölçtü.** Yukarıdaki
> *"1 satır"* yalnız **geçmişi olmayan** bir çalışan için doğruydu. (a) Kişinin
> mevcut en yeni satırının **altına** bir zaman beyan et → öncül hiç ilerlemez →
> yine **20 sayılan satır**; (b) **tek** bir ileri tarihli POST (`sys:occurred-at-bound`
> reddeder ama §4.6 satırı **yazar**) `ORDER BY occurred_at`'i kazanır, gap
> **negatif** olur, guardrail `gap >= 0` istediği için **hiç ateşlemez** → sonraki
> 20 dürüst tap **20 `ok`** (flag bile değil). Kural bu yüzden genişletildi:
> sunucu bacağı artık **ayrı bir sorgudan** (`SecondsSinceLastRecordedTap`,
> `ORDER BY created_at`, `channel IN ('nfc','qr')`, **yaşı SQL hesaplıyor**)
> okunuyor ve negatif beyan bacağı **düşürülüyor**. Ölçüm: (a) → **1 sayılan + 19 `ignored`**,
> (b) → **0 sayılan, 20'si de `sys:person-debounce`**. Üç katmanın tamamı:
> [ADR 0006](../adr/0006-debounce-iki-kosullu-zaman.md).
> **M5-10 devir cümlesi de buna göre düzeltildi:** pencere artık tavanı
> belirlemiyor (debounce belirliyor); yaptığı iş **imzalı bağlamın ömrünü**
> kısaltmak ve `sys:tap-freshness`'in bandını **erişilebilir** kılmak — yani
> M5-10 hâlâ gerekli, ama artık *"QR tekrarını sınırlayan tek şey"* değil,
> ikinci katman.
>
> **Aşağısı kural uygulanmadan ÖNCEki fizibilite ölçümüdür; tarihsel kayıt olarak
> duruyor ve ADR 0006'nın "Bağlam" bölümüne kaynaklık etti:**
> - **Veri bir katman aşağıda ZATEN VAR:** `GetLastTransactionForEmployee`
>   `created_at`'i **seçiyor** (`db/queries/transactions.sql`), ama
>   `checkin.transaction()` eşlemesi onu **düşürüyor** ve `tap.Transaction`'da
>   böyle bir alan **yok** (yalnız `OccurredAt`). Yani teknik maliyet üç satır.
> - **Geri tarih odası 72 saat** (ölçüldü: 71sa50dk → **200**,
>   `flag`/`base:queued-window`; 72sa10dk → **`reject`/`sys:occurred-at-bound`**).
>   Yani `sys:occurred-at-bound` kapıyı kapatmıyor, **genişliğini** belirliyor.
> - **Değişikliğin bedeli semantik:** debounce'un girdisini değiştirmek §5 satır
>   5'in anlamını ("aynı **kişi** 60 sn içinde tekrar") ve M4 karar motorunu
>   değiştirir; çevrimdışı kuyruk (M9-01) senaryosunda meşru bir toplu
>   senkronizasyonu da yutabilir. **Yapıcı bunu UYGULAMADI**; karar kullanıcının.
>
> **§4.6 açısından kayıp yok** (her satır yazılıyor, atfedilebilir; hepsi `flag`
> olduğu için müdür onayı olmadan saate girmiyor) — kaybolan şey §5'in *fiziksel
> dokunuş* ilkesi.
> **M5-08 pencereyi DARALTMADI** ve daraltmamalı: `sys:tap-freshness` zaten
> `tap:pageAgeSeconds` ile besleniyor (M5-05 F2) ve bandının boş olmasının **tek
> sebebi** TTL == guardrail tavanı (900 == 900).
> ⚠️ Bu **A1'i (URL biriktirme) çözmez** — ADR 0005'te kabul edilmiş risk, ve
> QR'da `tap:ctrGap` sinyali de **yok** (sayaç yok), yani Q21'in tek gözlemlenebilir
> işareti bu kanalda hiç üretilmiyor. Yazıldı, kapatılmadı.
>
> **5) Plaket baskı notu skill'e yazıldı, karta değil** (`tappa-brand` →
> *"Plaket baskısı — NFC + QR"*). Gerekçe: marka skill'i **fiziksel yüzeyin de**
> sahibi (kitchen docket motifi, palet, ses tonu orada) ve notu okuyacak olan
> M8-05 (encode runbook) ile M8-07 (pilot) skill'e bakar, bu karta değil; ayrıca
> §11 *"herhangi bir UI işi → skill `tappa-brand`"* diyor. Blok **"TASARIM ÖNERİSİ,
> ölçüm değil"** diye başlıyor ve ölçülmüş olan **tek** şeyi ayrıca işaretliyor:
> QR URL'i NFC URL'inin `&ctr=`/`&cmac=`'siz hâlidir (75 → **42** karakter,
> `https://time.tappa.mt` tabanıyla). Milimetreler baskı provasında doğrulanacak.
> Metin kararı: **`Hold your phone here` / `Or scan — same thing`** — *"backup"*,
> *"fallback"*, *"if your phone can't"* açıkça yasaklandı.
>
> **6) "QR ikinci sınıf gösterilmiyor" ÖLÇÜLDÜ: bugün HİÇBİR ekran QR'dan
> BAHSETMİYOR.** `TestQRScreens_SayNothingAboutTheQRRoute` tap + aktivasyon
> ailesinin **tamamını** (11 `Problem` sabiti × 2 retry hâli + `Tap` + `Activate` +
> `Confirm` + `Done` + 3 tur slaytı + 8 sonuç ekranı şekli) render edip `screenText`
> ile tarıyor: `qr` · `scan the` · `barcode` · `point your camera` → **0 eşleşme**.
> Yani kriter bugün **yoklukla** sağlanıyor. Test bir **tripwire**: biri bu
> ekranlara QR cümlesi eklerse kırılır ve mesajı §9'u hatırlatır (pozitif kontrol:
> `tapProblemUnknownTag`'e *"scan the QR code"* konunca **RED**).
> ⚠️ **Ölçülmüş sınır — kelime listesi neden kısa:** akış *"fall back"* ifadesini
> **zaten meşru** kullanıyor (`pages.Activate`: *"Without the network, taps fall
> back to your location"*, GPS hakkında). Genel başarısızlık sözlüğünü yasaklamak
> **yanlış KIRMIZI** üretiyordu (ilk sürüm tam da bununla patladı), o yüzden test
> yalnız **bu yola özgü** sözlüğü tarıyor.
> 🔴 **BULGU VE KULLANICI KARARI (2026-08-01): CÜMLE ŞİMDİ EKLENMİYOR.** Tur
> slayt 1 bugün *"If your phone does not react, ask your manager."* diyor. iPhone
> X ve öncesi arka planda NFC etiketi okuyamaz, yani o çalışan için telefon
> **hiçbir zaman** tepki vermeyecek: cümle onun **her günkü** yolunu bir arıza
> gibi çerçeveliyor ve müdüre yönlendiriyor. Doğru cümle muhtemelen *"Or scan the
> code on the plaque — it works the same way."*
> **Karar: EKLENMEDİ, ve gerekçesi bu görevin kendi bulgusuyla aynı sınıf.**
> **QR henüz fiziksel plakette basılı değil** — baskı spesifikasyonu bu görevde
> yeni yazıldı (md. 5), plaketler **M8** pilotunda üretilecek. Bugün o cümleyi
> koymak ekranın **var olmayan bir şeyi** çalışana söylemesi olurdu; §4.6'nın
> *"ekran, doğru olmayan bir şeyi beyan etmesin"* sınıfı, tam da bu oturumun
> avladığı kusur. **Kod değişmedi, ekran metnine dokunulmadı.**
> **M8 devri (skill'e de yazıldı):** plaketler QR ile basıldığı anda **tur slayt
> 1 ve iPhone X yolu yeniden ele alınmalı**; eklenince M5-06'nın **üç beyaz
> listesi** (metin · eleman · referans), tur testlerinin beklenen metni ve
> `TestQRScreens_SayNothingAboutTheQRRoute` **birlikte** güncellenir — o test tam
> bu anı yakalamak için tripwire olarak bırakıldı. *(Orkestratör bunu `state.md`'ye
> M8 devri olarak yazıyor.)*
>
> **7) 🔁 MASKELENMİŞ MUTANT BULUNDU VE KAPATILDI (bu oturumda dördüncü kez).**
> `internal/sun/preview.go` adım 4 (`if !p.HasSUN()`) silindiğinde **13 paketin
> HEPSİ yeşil** kaldı (ölçüldü, tam suite). Kalabiliyordu çünkü **sonuç aynı**:
> QR `Params` nil CMAC taşır, `verifyMAC` boşluğa karşı karşılaştırır ve `false`
> döner — kısa devrenin döndürdüğünün aynısı. Sonucun sakladığı fark: **doğrulanacak
> hiçbir şey taşımayan bir URL için etiketin AES anahtarı AÇILIYOR** (§4.7'nin
> *"anahtar yalnız gerektiğinde ve doğrulama süresince yaşar"* kuralı; aynı kural
> adım 3'te ölü etiket için **pinli**, çünkü emekli bir etiket + geçerli CMAC
> `CMACValid`'i `true`'ya çevirip tabloyu kırardı — QR'ın böyle bir teli yoktu).
> **Kapatıldı:** `TestPreview_QRURLNeverUnwrapsTheTagKey` bozuk bir `aes_key_ref`
> ile anahtar açmayı **gözlemlenebilir** yapıyor (açan yol **hata** döner, kısa
> devre yapan yol `(CMACValid:false, nil)`), **pozitif kontrolüyle** (aynı bozuk
> ref + NFC URL → hata). Mutasyon artık **RED**.
>
> **7b) M5-09'A DEVİR — bozuk `aes_key_ref` NFC'yi kesiyor, QR'ı KESMİYOR
> (ölçüldü).** M5-04 devri md. 5, seed'li plaketlerin `aes_key_ref`'inin KEK-sarmalı
> **olmadığını** ve NFC yolunun onlarda **500** verdiğini yazıyor; kimse bunun QR'ı
> da öldürüp öldürmediğini ölçmemişti. Ölçüldü
> (`TestQRDB_ABrokenKeyRefBlocksNFCButNotQR`, aynı plakette bir bayt bozularak):
> **NFC `GET /t` → 500**, **QR `GET /t` → 200 + POST → `ok`/`channel='qr'` satırı**.
> Sebep md. 7'nin kısa devresi: QR yolu anahtara hiç uzanmıyor. **Sonuç:** M5-09'un
> *"bir günü simüle et"* senaryosunun **QR yarısı bugün seed'li plaketlerle
> HTTP üzerinden çalıştırılabilir**; NFC yarısı seed düzelene kadar bloklu kalır
> (backlog **T7** ile aynı kök).
>
> **8) Ölçülen bir davranış, iddia değil: `POST /api/checkin` MULTIPART GÖVDEYİ
> HİÇ OKUMUYOR.** `Checkin` önce `r.ParseForm()` çağırıyor; bu, multipart bir gövde
> için `r.PostForm`'u **nil olmayan ve boş** bırakıyor, `PostFormValue` ise
> `ParseMultipartForm`'a yalnız `PostForm == nil` iken uzanıyor. Sonuç: multipart
> bir POST'ta `ctx` de kayboluyor → **400 `tapProblemStale`, sıfır satır**. JSON
> gövde de aynı yere düşüyor. İkisi de **savunma olarak değil, ölçülmüş şekil**
> olarak yazıldı ve teste bağlandı; bu endpoint bir gün multipart okumaya başlarsa
> iki vaka da kanal-beyanı tablosuna **taşınmalı**. *(İlk test sürümü multipart'ın
> "servis edilip beyanı yok sayacağını" varsaymıştı; ölçüm 400 dedi ve varsayım
> düzeltildi.)*
>
> **8b) DOSYANIN DEĞERİ RAPORDA AZ SÖYLENMİŞTİ — denetim ÜÇÜNCÜ bir maskeli
> mutant ölçtü.** `handler/checkin.go`'daki `Channel: tap.Channel(tctx.Channel)`
> eşlemesi sabit `nfc`'ye çevrildiğinde, `qr_db_test.go` **olmadan** tüm suite
> **yeşil** kalıyor: yani M5-08 öncesinde *"kanal sunucudan türetiliyor"*
> garantisinin **sütuna ulaştığını** hiçbir şey kanıtlamıyordu (var olan test
> `TestCheckin_TagFactsComeFromTheSignedContextOnly` bir **fake** servisin gördüğü
> alanı okuyor, `transactions.channel`'ı değil). Aynı sınıf iki yerde daha:
> `ctr` sütununun QR'a da yazılması ve kanal beyanının **header / harf-varyantı**
> yüzeyleri — üçünü de yalnız bu dosya yakalıyor. Yani dosyanın kapattığı boşluk
> *"QR yolu test edilmemişti"*den daha geniş: **kanalın kayda ulaşması** hiç
> pinlenmemişti.
>
> **Mutasyonlar — 13 deneme: 11 tam RED, 1 kısmi, 1 TAM YEŞİL (kapatıldı).**
> Tam RED olanlar: `base:qr-requires-ip` `review`→`allow` · `Channel` eşlemesi sabit
> `nfc` · istemci kanalı `PostFormValue` ile · `FormValue` ile (query'yi de kapsar) ·
> header ile · `sun_valid` sütunu hiç yazılmıyor · `ctr` sütunu QR'a da yazılıyor ·
> `sun.Parse` SUN'sız URL'e `nfc` diyor · person-debounce beslenmiyor · ekrana
> *"scan the QR code"* cümlesi eklendi · `tapContextTTL` 15→5 dk.
> **TAM YEŞİL kalan tek mutant** md. 7 (preview adım 4, 13 paketin hepsi `ok`) —
> kapatıldı, artık **iki** pakette RED.
> **1. denetim turunda iki mutasyon daha koşuldu** (yeni/yeniden yazılan iki testin
> boş olmadığını göstermek için, **ikisi de RED**): `decide.go`'da debounce gap'i
> sabit `0`'a çekildi → 20 tekrarın hepsi `ignored` oldu, `…DefeatsTheDebounce…`
> **RED**; `httpx.BySession` hiç ücretlendirmiyor → `served=303 refused=0`,
> `…TheExposureCeilingIsTheSessionBudget` **RED**.
> ⚠️ **KISMİ (dürüst nüans):** `advance`'in `req.Channel != tap.ChannelNFC` koşulu
> silindiğinde `internal/domain/checkin` **RED** oluyor (M5-05'in `advance_test.go`'su),
> ama **`internal/handler` YEŞİL** kalıyor — çünkü QR'ın `ctr`'ı 0 ve
> `WHERE last_ctr < 0` zaten hiç satır tutmuyor. Yani bu koruma HTTP seviyesinden
> **gözlemlenemiyor**; onu tutan tel alt katmanda ve orada duruyor.
>
> **Açıkta bırakılan sınırlar (iddia değil, sınır):**
> - **QR'da `tap:ctrGap` yok** → A1'in tek gözlemlenebilir sinyali (Q21,
>   `base:ctr-gap-review`) bu kanalda **hiç üretilmiyor**. M6-11'in tespit
>   sinyalleri QR trafiğini bu telle göremez.
> - 🔴 **GERİ ÇEKİLDİ (1. denetim turu):** burada iki cümle vardı ve **ikisi de
>   ölçümün tersini** söylüyordu — *"`occurred_at` bir açık değil"* ve *"tavan 16
>   bir aritmetiktir; gerçek trafikte **daha düşük** sayılar üretir"*. Ölçüm
>   **daha yüksek** dedi (0,51 sn'de 20 satır; yapısal tavan ~599) ve mekanizma
>   tam olarak `occurred_at`. Doğrusu md. 4'te.
> - **~600 / ~599 YAPISAL BİR TAVANDIR, gözlenmiş bir sayı değil.** Gözlenen iki
>   şey: *20 satır 0,51 sn'de, 0 `ignored`* ve *300 istek servis edildi, 301. 429*.
>   Aradaki *"15 dk'ya iki pencere sığar"* adımı **aritmetik** — 600 isteklik bir
>   koşu yapılmadı, çünkü `transactions` silinemez ve paylaşılan dev DB'ye
>   600 satır yazmak M5-05'in *"yazma bütçesi"* uyarısının kendisi olurdu.
> - 🟢 **Debounce'a sunucu saati vermek UYGULANDI** (kullanıcı kararı 2026-08-01,
>   [ADR 0006](../adr/0006-debounce-iki-kosullu-zaman.md)). Migration gerekmedi.
> - 🔴 **YENİ, AÇIK BEDEL: M9-01 (çevrimdışı kuyruk) bu kuralla ÇARPIŞIR.** Aynı
>   senkronda saniyeler arayla gönderilen iki kuyruklanmış tap'in ikincisi
>   `created_at` bacağından `ignored` olur, oysa kişi gerçekten iki kez tap
>   etmiştir. Sessizce feda edilmedi: M9-01 kuyruk boşaltmasını sunucunun
>   **doğrulayabileceği** bir işarete bağlamak zorunda (M5-10 kartı `queued`
>   damgası + `base:queued-window`'u zaten bu ayrım için ayırıyor). ADR 0006
>   "Sonuçlar" md. 1.
> - **Repodaki DÖRT test bu deliğe yaslanıyordu** (`tapAgo(900)`, `at(-300s)`) ve
>   yorumları bunu açıkça söylüyordu — deliğin gerçek ve erişilebilir olduğunun
>   ayrıca kanıtı. Geçmiş artık beyan edilerek değil **yazılarak** kuruluyor
>   (`seedTapAgedBy`/`seedAgedRecord`, iki damga birlikte geriye alınıyor);
>   ölçülen tap her vakada hâlâ gerçek bir POST, hiçbir iddia zayıflatılmadı.
> - **`channel` hiçbir kullanıcı yüzeyinde görünmüyor** (md. 3 sonu) — M6 devri.
> - **Migration yok, şema değişmedi, yeni bağımlılık yok.** Bu görev yalnız test,
>   bir belge bloğu ve skill bölümü ekledi; **üretim kodu değişmedi**.
>   ⚠️ **BU CÜMLE ARTIK YANLIŞ** ve düzeltilmeden bırakılamaz: ADR 0006 kararı
>   (2026-08-01) üretimde **üç Go dosyası + bir sorgu** değiştirdi —
>   `internal/domain/tap/{decide.go,types.go}`, `internal/domain/checkin/checkin.go`
>   ve `db/queries/transactions.sql` (+ üretilen `internal/store`). **Migration
>   yine yok.** Cümle görevin ilk yarısı için doğruydu; ikinci yarısı kod işiydi.
> - 🔴 **KİLİDİN İLGİSİZ KİŞİLERE BEDELİ VAR** (kırmızı çizgi denetimi, ölçüldü).
>   Bekleyen istek **havuz bağlantısını tutuyor**: tek anahtara inen bir flood'da
>   16 bağlantının **15'i** `wait_event='advisory'`, ayrı anahtarlarda **0**.
>   Temiz A/B'de ilgisiz birinin **tek** tap gecikmesi **6–9×** kötüleşiyor
>   (1,60/1,05/1,14 sn vs 0,178 sn). §4 ihlali değil (kayıt kaybı yok, tavanlar
>   `ByAddress` 3000/10dk ve `middleware.Timeout(30s)`) ama **yeni bir yüzey**;
>   *"kilide atfedilebilir DoS yok"* cümlesi geri çekildi. **Yöntem uyarısı:**
>   flood'u tek oturumdan sürmek isteklerin çoğunu `BySession` 300/10dk'ya takıp
>   429 yaptırır ve "fark yok" artefaktı üretir — flood ayrı oturumlardan, kurban
>   **tek atış** ölçülmeli.
> - 🔴 **DAĞITIM DEVRİ (M8):** küme, DB ve rol seviyesinde `statement_timeout`,
>   `lock_timeout`, `idle_in_transaction_session_timeout` **üçü de 0** (ölçüldü).
>   Tek tavan `middleware.Timeout(30s)`.

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

> **Kart düzeltmesi (2026-08-01, M5-09 uygulaması sırasında; md. 6 ve md. 2'nin
> son paragrafı 2026-08-02, 2. turda eklendi).** Altı nokta gerçekle çelişti;
> hiçbiri kriter zayıflatılarak değil, **ölçülerek** düzeltildi.
>
> **1) "~21 işlem" ARİTMETİK OLARAK MÜMKÜN DEĞİL — ve sebebi §5'in kendisi.**
> §5 *"aktivasyon sonrası ilk kayıt `practice=true`"* diyor. Taze seed'li bir
> veritabanında **hiç kimsenin** kaydı yoktur, yani tap eden her çalışanın ilk
> satırı bir TRAINING satırıdır. 10 çalışanın 9'u tap ediyorsa (biri telefonsuz,
> manuel giriliyor) taban zaten 9 + 9 giriş + 9 çıkış = 27'dir. Ölçülen gerçek gün
> **31 satır** — ve dökümü **kayıt** olarak yazılmalı, "giriş/çıkış" olarak değil
> (2. turda düzeltildi: `ignored` çift tap ve retired plaket reddi **hiçbir yön
> taşımaz**, `type` sütunları NULL'dır; onları "giriş"/"çıkış" saymak aritmetiği
> doğru, adlandırmayı yanlış yapıyordu):
> **9 practice + 1 deaktive denemesi (reddedildi, yönsüz) + faz 1'de 11 kayıt**
> (6 NFC giriş · 1 `ignored` çift tap · 1 QR giriş · 1 manuel giriş · 1 Rusty Bar
> 18:05 · 1 Grace'in ilk gerçek girişi) **+ faz 2'de 10 kayıt** (7 NFC çıkış ·
> 1 QR çıkış · 1 Rusty Bar 02:10 · 1 retired plaket reddi, yönsüz) = **31**.
> Kriter *"~21"* yerine **"her senaryo dahil, sayı senaryoların toplamıdır"**
> okunmalı; test toplamı (31) açıkça iddia ediyor ki bir senaryo sessizce düşerse
> sayı değişsin.
>
> **2) "Geç kalma → rapor 'late 17m'" BUGÜN ÜRETİLEMİYOR — iki ayrı sebep,
> ikisi de ölçüldü.** (a) `tap.Decision.MinutesLate` **hiçbir yere yazılmıyor**:
> ne `transactions` sütunu, ne `policy_context` anahtarı (`time:minutesLate`
> `document.go`'da tanımlı ama `Decide` onu **set etmiyor**), ne onay ekranı. Yani
> HTTP üzerinden bir tap'in geç kalması **gözlemlenemez**. (b) Daha ağırı:
> `lateness()` mesafeyi `in.Now` — **sunucu saati** — ile ölçüyor, kaydın
> `occurred_at`'i ile değil. Canlı tap'te ikisi çakıştığı için hiçbir mevcut test
> bunu görmüyor (`lateness_test.go` `OccurredAt`'i **hiç** set etmiyor). Ölçüm:
> 10:00 vardiyasına 10:17 beyan eden manuel bir giriş `MinutesLate = -520`
> döndürdü (= o anki sunucu saati − bugünün 10:00'ı). Geriye tarihli müdür girişi
> ve M9-01 çevrimdışı kuyruk için bu **yanlış**. Bu görevde **düzeltilmedi**:
> karar motoru semantiğini (M4-05, §5) değiştirir ve bugün üretimde sonucu yoktur
> (değer hiçbir yere yazılmıyor). M6-07 raporlarında **gerçek** olur. Bugünkü
> davranış `day_db_test.go`'da pinlendi; düzeltilirse test kırmızıya döner.
>
> ⚠️ **2. tur eki — "geç kalma" senaryosunun HTTP tarafında HİÇBİR kanıtı yok,
> ve 1. turda bunu gizleyen bir assertion vardı.** `assertAfterShiftStart`
> (kaydın `occurred_at`'i vardiya başlangıcından sonra mı) **yapısal olarak
> boştu**: karşılaştırılan an tanımı gereği `now − 20 dk`'dan eski, canlı tap'in
> `occurred_at`'i ise ≈ `now`. Ölçüldü — `tap.lateness` `return nil`'e mutasyona
> uğratılınca (yani geç kalma hesabı **tümden** ölünce) gün o satırdan sorunsuz
> geçti ve ancak çok sonra, manuel girişin `MinutesLate == nil` kontrolünde öldü
> (`day_db_test.go:440`, mutasyon anındaki numara). Assertion yeniden yazılmadı,
> **kaldırıldı**: hiçbir motor hatasının
> kırmızıya çeviremediği bir kontrol, kapsam gibi okunduğu için yokluğundan
> **daha kötüdür**. Senaryonun dürüst durumu: **HTTP'de üretilmiyor**; tek
> gözlem noktası, çağıranın `Decision`'ı elinde tuttuğu manuel giriştir ve o da
> yukarıdaki sunucu-saati okumasıdır. Skill `tappa-seed`'in senaryo tablosu ve
> `day_db_test.go`'nun LIMITS L2'si aynı şeyi söylüyor.
>
> **3) NFC tap'in "CMAC doğrulandı" biti SİMÜLASYONDA VARSAYILIYOR — sınır olarak
> sayıldı, kapatılmadı.** Sayfa gerçekten açılıyor (gerçek `sun.Parse`, gerçek
> KEK unwrap, gerçek preview) ama URL'deki CMAC kasten yanlış; simülasyon sunucunun
> kendi imzaladığı bağlamı alıp **tek biti** çevirip yeniden imzalıyor. Geçerli bir
> CMAC üretmek SDM yapısının `internal/sun` dışında **ikinci bir uygulaması**
> demekti (repo bunu daha önce iki kez reddetti — `tap_db_test.go`). Sonuç: bu
> dosya bir NFC tap hakkında **çipin imzasının doğrulandığı dışında her şeyi**
> kanıtlıyor; o yarısı `internal/sun`'ın kendi vektörlerinde duruyor.
>
> **4) Gün, gerçek zamanda BEKLEMEK ZORUNDA (~62 sn) — ADR 0006'nın bedeli.**
> Debounce mesafesi artık istemcinin erişemediği bir sunucu yaşıdır, yani aynı
> kişinin ardışık tap'leri arasında **gerçek** saniye geçmeli. Ölçüldü
> (`TestSeedDB_WithoutTheWaitTheDayCollapsesIntoIgnoredRows`): beklemesiz 15
> kaydın **10'u `ignored`**; tam güne uygulandığında 31 satırın 19'u, bütün
> çıkışlar dahil. Pencere `policy.DebounceMinSeconds` = **30 sn**'ye (bir
> deployment'ın kurabileceği en düşük değer) çekildi; daha azı `config.Load`
> tarafından reddediliyor, yani hiçbir kurulumun sahip olamayacağı bir
> konfigürasyonu sürmek olurdu.
>
> **SÜRE ÖLÇÜMÜ (2. tur) — üç okuma, KARAR KULLANICININ.** Bu görevden önce
> `make test` ~38 sn'ydi. Aynı makinede, `-race`, `-count=1`. ⚠️ **Mutlak sayılar
> ARALIKTIR, sabit değil:** dört oturumda `make test` **84,7 – 138 sn**, gün testi
> **62,9 – 64,3 sn**, `-short` **32,9 – 35 sn** verdi. Fark makine yüküdür ve
> **uyku dışındaki her şeyi** oynatır — gün testi süitin duruma göre yarısı ile
> üçte ikisi arasındadır; sabit olan yalnız içindeki ~62 sn'lik `time.Sleep`'tir:
>
> | | `make test` | `internal/handler` | kararlılık |
> |---|---|---|---|
> | **(a) bugünkü hâl** | **84,7 – 138 sn** | 91,6 sn (gün testi tek başına **62,9 – 64,3 sn**; ≈62'si `time.Sleep`) | 3/3 + 3. turda 2/2 yeşil |
> | **(b) `-short` ile gün testi atlanır** | **32,9 – 35 sn** | 27,7 sn | yeşil — ama **atlanan bir test geçmiş sayılmaz** (M5-06); varsayılan koşu artık zinciri hiç sürmez |
> | **(c) `t.Parallel()`** | — | **3/3 KIRMIZI** | ⛔ kullanılamaz |
>
> ✅ **KULLANICI KARARI (2026-08-02): "`make test` tam kalsın + `make test-short`
> eklensin."** Uygulandı, 3. turda. `make test` ve CI (`make check` → `make test`)
> **değişmedi** — hiçbir şey atlanmıyor. `make test-short` (`-short`) yalnızca
> geliştiricinin iç döngüsü içindir ve tek bir testi atlar:
> `TestSeedDB_ADayAtKFStJulians`. Skip **yüksek sesle** neyin koşmadığını adıyla
> söyler ve "atlanan test geçmiş sayılmaz" dersini (M5-06 md. 4) tekrarlar; bu,
> repodaki **ilk** `testing.Short()` kullanımıdır, o yüzden gerekçesi testin
> başında yazılıdır. İki hedefin neden var olduğu (üç sayı + `t.Parallel()`'ın
> neden elendiği) `Makefile`'da `test-short`'un üstüne de yazıldı.
> **Ölçüm (3. tur):** `make test` → 138 sn, 13 paket, **1255 PASS / 0 SKIP /
> 0 FAIL**; `make test-short` → 35 sn, 13 paket, **1254 PASS / TAM 1 SKIP /
> 0 FAIL** ve o tek skip `TestSeedDB_ADayAtKFStJulians`. Yani `-short` süitte
> başka hiçbir şeyi sessizce düşürmüyor.
>
> **(c) neden çöktü — ölçüldü, tahmin değil.** Paketin bütün üst düzey testlerine
> `t.Parallel()` eklendi (zaten paralel olanlar hariç) ve `-race` ile **art arda
> üç koşu, üç FARKLI testte kırmızı**: `TestSeedDB_ADayAtKFStJulians`
> (*"the first record after activation must be a TRAINING tap"*) ·
> `TestSeedDB_ANFCTapOnASeededPlaqueReachesTheDecisionEngine`
> (*"last_ctr moved 192 → 193 on an unverified MAC (§4.4)"*) ·
> `TestSeedDB_WithoutTheWaitTheDayCollapsesIntoIgnoredRows`
> (*"tap 1 = reject, expected ok or ignored"*). Üçünün de kökü aynı: bu testler
> **aynı seed'li plakete** dokunuyor ve her tap `last_ctr + 1` okuyor; paralel
> koşuda sayacı başka bir test ilerletince tap **replay** sayılıp reddediliyor.
> İkincisi özellikle önemli: paralellik bir **kırmızı çizgi** testine (§4.4)
> olmayan bir ihlali rapor ettiriyor — yani sessiz bir yavaşlama değil, **yanlış
> alarm üreten** bir kurulum olurdu. Üstelik kazanç en iyi ihtimalle sınırlı:
> paketin 91,6 sn'sinin 63,7'si tek testin uykusu, geri kalan **28 sn**'nin
> tamamı gizlenebilse bile `make test` ancak ~70 sn'ye iner. Paralellik uykuyu
> **kısaltamaz**, yalnız arkasına iş saklayabilir. Çalışır hâle getirmek, plaket
> sayacına test-içi bir serileştirme (ya da test başına ayrı plaket) demek —
> kendi görevi.
>
> **5) `manual` kanalının HTTP yüzeyi YOK (M6-04), ve geriye tarihli manuel giriş
> `flag` oluyor.** Manuel kayıt bu yüzden router'ın kullandığı **aynı** servisten
> (`checkin.Service.Record`) üretiliyor — uydurulmuş bir endpoint değil, elle
> yazılmış bir satır da değil. Yan ölçüm: müdürün sabahki mesaiyi girmesi
> `base:queued-window`'un 120 sn eşiğini aştığı için kaydı **`flag`** yapıyor, yani
> müdürün kendi girdiği satır kendi onay kuyruğuna düşüyor. Bug değil, **ürün
> kararı** — M6-04'ün çözmesi gereken bir şey olarak yazıldı.
>
> **6) 🔴 BULGU (2. tur) — PRACTICE SATIRI DAHA ESKİ BİR AÇIK GİRİŞİ MASKELİYOR.
> BU GÖREVDE DÜZELTİLMEDİ: kendi görevini ve muhtemelen bir ADR'yi hak ediyor.**
>
> **Mekanizma.** `GetLastOpenTransaction` **tek** satır döndürüyor
> (`type='in'` + daha yeni bir `'out'` yok, `ORDER BY occurred_at DESC LIMIT 1`)
> ve practice'e **nötr** — bilinçli, sorgunun kendi doküman bloğu bunu söylüyor.
> Ayıklamayı `internal/domain/checkin/checkin.go`'nun `gather`'ı yapıyor
> (≈ satır 937): `if !open.Practice { f.lastOpenIn = transaction(open) }`. Yani
> dönen satır practice ise **atılıyor ve bir alttakine BAKILMIYOR**. Sonuç:
> `occurred_at` sıralamasında en üste düşen bir practice satırı, altındaki
> **gerçek açık girişi görünmez** yapıyor ve `lastOpenIn` `nil` kalıyor.
>
> **Ölçüm (2. turda, kontrol koluyla yeniden üretildi).** İki kişi, üçer tap,
> her tap **düz HTTP** (`GET /t` + `POST /api/checkin`); tek fark practice
> satırının `occurred_at`'i:
>
> | kol | practice `occurred_at` | 3. tap'in yönü | kalan açık giriş |
> |---|---|---|---|
> | kontrol | beyan: −4 sa (gerçek girişten **eski**) | `out` | **0** ✓ |
> | bozuk | beyansız = sunucu şimdi (**yeni**) | `in` | **2** 🔴 |
>
> (İki kolun gerçek girişi de −3 sa beyan edildi, ikisi de `verdict=flag`;
> 3. tap'ler canlı ve `verdict=ok`.)
>
> **İhlal edilen cümle — §5:** *"Yön (in/out): kişinin **son açık girişine** göre
> toggle."* Bozuk kolda çıkış `in` oldu, gerçek giriş **hiç kapanmadı** ve bunun
> bir motor hatası olduğuna dair **hiçbir sinyal yok**: `verdict` `ok`, not yok,
> flag yok, kuyruk yok. Müdüre §5'in *"unutulmuş çıkış"* anomalisi gibi
> görünüyor — yani **çalışanın hatası** gibi.
>
> **Düz HTTP'den erişilebilir; manuel kanal gerekmiyor.** `occurred_at` sevk
> edilmiş `POST /api/checkin`'in **zaten kabul ettiği** bir form alanı ve 3 saat
> `sys:occurred-at-bound`'un toleransının (ADR 0004 §11 sınırlı parametre,
> `policy.OccurredAtSkewMaxSeconds` = 72 sa) rahat içinde. Kötü niyet de
> gerekmiyor: **beyansız** (yani sıradan) bir practice tap'i sunucu şimdi'sine
> yazılır, ve geriye tarihli tek bir gerçek giriş yeterlidir.
>
> **Gün fixture'ı bunun ETRAFINDAN DOLAŞIYOR, kapatmıyor.** Ivan'ın practice
> tap'i 17:50 **beyan ediliyor** ki 18:05 girişinin altında sıralansın; bu
> olmadan 02:10 çıkışı `in` okunurdu — yani gece vardiyası regresyon testinin
> kendisi bu bulguya çarpıyor. Çare çağrı yerinde ve `day_db_test.go`'nun
> **LIMITS L3** bölümünde açıkça yazılı; sessiz bırakılsa fixture "motor sağlam"
> kanıtı gibi okunurdu.
>
> **Neden burada düzeltilmedi.** Düzeltmenin kendisi bir **karar**: practice'i
> atlayıp bir alttakine mi bakılacak (sorgu `LIMIT 1`'i bırakır), sorgu
> `AND NOT practice` ile mi daraltılacak (o zaman "practice-nötr" doküman bloğu
> ve M4-04'ün iki-koruma gerekçesi değişir), yoksa practice satırı hiç `type`
> taşımamalı mı (o zaman §5'in *"ilk kayıt practice"* satırı ile `transactions`
> tutarlılık CHECK'i yeniden konuşulur). Üçü de karar motoru semantiği + saat
> hesabı kesişimi. **Hiçbir test bu davranışı pinlemiyor**; yeniden üretme tarifi
> yukarıdaki tablo ve LIMITS L3.
>
> ⚠️ **3. tur düzeltmesi — pinlememenin MALİYET gerekçesi düşüyor, DONDURMA
> gerekçesi ayakta.** Yukarıda "pinlemek iki debounce beklemesi = ~62 sn daha
> demekti" yazıyordu; **yanlış**, ve karşı kanıt aynı dosyada: debounce'un
> sunucu-yaşı bacağı yalnız `channel IN ('nfc','qr')` satırlarını sayıyor
> (`db/queries/transactions.sql` → `SecondsSinceLastRecordedTap`, manuel muafiyet).
> Yani üç satır `checkin.Service.Record` ile **`manual`** kanalından beyan
> edilerek yön davranışı **~0 sn**'de pinlenebilirdi — ve `day_db_test.go` Nadia
> için o yolu zaten kullanıyor. Geriye tek gerçek gerekçe kalıyor: pinlemek
> **düzeltilecek bir kusuru dondururdu**.
>
> ✅ **KULLANICI KARARI (2026-08-02): "şimdi düzelt — yeni M5 görevi."** Bu bulgu
> kendi görevinde (**M5-11**) düzeltilecek; M5-09 motoru değiştirmedi, yalnızca
> etrafından dolaştı ve bunu yüksek sesle yazdı.
>
> ⚠️ **2. tur eki — simülasyonun ürettiği kadro artık DAMGALI, ve seed'li kadroyu
> sürmeme kararı ölçüldü.** Gün, seed'li isimleri (Maria Borg, …) taşıyan
> **taze** çalışanlar yaratıyor; `transactions` değişmez (§4.3), `tappa_app`'in
> DELETE yetkisi yok, yani hiçbir şey onları temizlemiyor — ölçüldü: birkaç
> `make test` sonrası demo tenant'ında **13 tane "Maria Borg"** vardı, tap'ler
> onlardaydı, seed'li asıl Maria Borg'un **0 işlemi**. Alternatif (a) — günün
> **seed'li kadroyu** sürmesi, ki skill'in *"dashboard'u gerçekçi göster"*
> amacını gerçekten karşılardı — sabit kimlikli bir sonda ile **iki koşuda**
> ölçüldü: 1. koşu (`rows=0, status=invited`) davet + aktivasyon + tur çalışıyor,
> ilk tap `practice=true`; 2. koşu (`rows=1, status=active`) aktivasyon **turu
> atlıyor** (`/activate/done`'a düşüyor, ilk slayt hiç render edilmiyor), ilk tap
> `practice=false`, üstelik koşu 1'den **açık bir giriş devralıyor**. Yani sabit
> kadro bu günü veritabanı ömrü başına **tam bir kez** ağırlayabilir; `make test`
> yeniden koşabilir olmak zorunda ve ikinci koşuda `t.Skip`'e düşmek bu repoda
> kabul edilmiyor (*"atlanması geçme değildir"*, M5-06). Seçim (b): taze kadro +
> `full_name`'e **koşum damgası** (`Maria Borg [sim 08-02T00:40:24 f4ef]`), yani
> satırlar **ne olduklarını kendileri söylüyor** ve M6 dashboard'u ana veriyi
> simülasyon çıktısından ad üzerinden ayırabiliyor. Skill'in *"dashboard'u
> gerçekçi göstermek"* cümlesi de ölçüme eşitlendi.
> Damganın kendisi **korumasızdı** (ölçüldü: kaldırınca süit tamamen yeşil kalıyor
> — `full_name`'e hiçbir test bakmıyor), o yüzden bir iddiaya bağlandı:
> `assertTellableApart` günün başında, hiçbir bekleme öncesinde koşuyor ve hem
> damgayı hem de *"seed'li satır AYRI bir satırdır, üzerine yazılmadı"* şartını
> kontrol ediyor. **Mutasyon (`stamped := name`) → 0,17 sn'de KIRMIZI.**
> ⚠️ Bu ölçümler dev veritabanında damgasız birkaç çift bıraktı; demo/ekran
> görüntüsü öncesi **`make db-reset`**.
>
> ⚠️ **3. tur — o iddia damganın TEKİLLİĞİNİ korumuyordu.** `assertTellableApart`
> yalnız `stamped := name` mutasyonunu yakalıyor; `newRunStamp()`'ı
> `return "CONSTANT"` yapan mutasyonda süit **yeşil kaldı** (ölçüldü). Sebep
> dosyanın kendi adını koyduğu tuzak: karşılaştırmanın iki yakası da aynı
> `f.runStamp`'tan türüyor, yani damga kendisiyle kıyaslanıyor. Kapatıldı:
> (i) `assertTellableApart` artık damgalı adın veritabanında **tam 1** satıra ait
> olduğunu da soruyor; (ii) `TestRunStampVariesBetweenRuns` fonksiyona doğrudan
> soruyor (DB'siz, `-short`'ta da koşar). **Mutasyon `newRunStamp → "CONSTANT"`
> artık KIRMIZI.** Kalan kalıntı yazıldı: damga bir saniyelik saat + 16 bit UUID,
> yani aynı saniyedeki iki koşu **1/65536** olasılıkla çakışır.
>
> 🔭 **DEĞERLENDİRİLMEMİŞ ÜÇÜNCÜ YOL (sahibi M6, kayda geçsin).** (a) ve (b)
> arasındaki gerilim, günün **`make test` içinde** olmasından doğuyor: yeniden
> koşulabilirlik zorunlu olduğu için kadro taze olmak zorunda, taze olduğu için
> de dashboard seed'li isimlerle dolmuyor. Üçüncü yol ikisini ayırır: günü
> `make test`'ten **çıkar** (bugün `testing.Short()` yalnız `-short`'ta atlıyor —
> bu daha ileri bir adım), ve seed'li kadroyu **yalnız `make simulate-day`**
> içinde sür. O zaman `make test` her koşuda yeşil kalır, `make simulate-day` ise
> veritabanı ömrü başına bir kez çalışan, **ekran görüntüsüne hazır** bir gün
> üretir (ikinci koşu için `make db-reset`). Bedeli: günün regresyon değeri
> varsayılan koşudan çıkar — yani M5-06'nın *"atlanan test geçmiş sayılmaz"*
> dersiyle doğrudan çelişir ve bu bir **ürün/CI kararıdır**, bu görevin değil.
> Ölçülmedi; M6 dashboard'u seed'li veriye baktığında karar verilmeli.
>
> **Ayrıca bu görevde kapatılan bir bloke:** seed'li plaketlerin `aes_key_ref`'i
> KEK-sarmalı değildi (42 bayt ASCII placeholder) ve seed'li her NFC tap
> `GET /t`'de **500** veriyordu (yeniden üretildi, sonra düzeltildi ve pinlendi).
> Sarmalama artık `scripts/seed.sh`'ın 2. adımı; anahtarlar **türetiliyor**
> (`fixtures.SeedTagKey`), repoda hiçbir sarmalı değer ya da gerçek anahtar yok.
> Backlog **T7**'nin *seed* yarısı kapandı ve mekanik olarak korunuyor (seed adımı
> 44 baytlık zarfı olmayan demo plaket bırakırsa `RAISE` ile patlar).
>
> **Açık kalan yarısı — ve 2. turda düzeltilen bir karamsarlık.** 1. turda
> *"şema `bytea`ya bu bir KEK zarfıdır dedirtemiyor"* yazmıştım; bu **fazla
> genişti**. Şema zarfın **AÇILDIĞINI** zorlayamaz (KEK veritabanında değil, ki
> öyle olmalı — ADR 0003), ama **ŞEKLİNİ** zorlayabilir:
> `CHECK (octet_length(aes_key_ref) = 44)` 44 dışındaki her değeri, bu görevin
> yeniden ürettiği 500'ün tam sebebini, **insert anında** reddederdi. Bunu bugün
> engelleyen şey şema değil, **iki adımlı seed'in ara durumu**: adım 1 düz ASCII
> bir placeholder yazıyor (bugün **50 bayt**: 36 karakterlik etiket + 14 hex uid,
> `octet_length` ile ölçüldü) ve adım 2 onu sarmalıyla değiştiriyor — CHECK
> olsaydı adım 1 patlardı, Postgres'te CHECK `DEFERRABLE` de olamaz.
> **T7'nin sahibine (M6/M7) net seçenekler:** (i) placeholder'ı tam **44 bayta**
> çek, CHECK'i ekle (en ucuzu; placeholder'ın "yer tutucu" olduğunu metin yerine
> uzunluk-dışı bir işaret taşısın); (ii) adım 1 `tags` satırını `aes_key_ref`
> **olmadan** yazsın — sütun bugün `NOT NULL`, yani ya nullable olacak ya da
> plaket satırı tümüyle adım 2'de doğacak; (iii) CHECK yerine sarmalama/çözme
> **yalnız `internal/sun` üzerinden** kuralını insert yolunda testle koru
> (bugünkü fiili durum). T7 **daraldı, kapanmadı**; ölçülmemiş tek şey (i)'in
> seed dışındaki insert yollarına maliyeti.

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

> **Kart düzeltmesi (2026-08-02, M5-10 uygulaması).** Yukarıdaki kriterler
> **context olarak duruyor**, silinmedi — ama kartın yarısı **M5-04'ten ÖNCE**
> yazılmış ve o görev **imzalı tap bağlamını** getirdiği için sekiz cümlesi
> ölçümle yanlışlandı. Aşağısı ne yapıldığını ve neden kartın tasarımının
> **uygulanmadığını** yazar.
>
> **NE YAPILDI — küçük ve odaklı.** `TAPPA_FRESHNESS_SECONDS` (varsayılan
> **180 sn**, aralık `policy.FreshnessMin/MaxSeconds` = 60..900, aralık dışı
> değer **başlangıç hatası**) config'e girdi ve `checkin.New` onu
> `params.FreshnessWindow`'a bağladı. Guardrail `sys:tap-freshness` M3-05'te,
> `tap:pageAgeSeconds` beslemesi M5-04'te zaten inmişti; **eksik olan tek şey bir
> parametreydi** — ve o parametre eksik olduğu için guardrail pratikte hiç
> ateşlenemiyordu: `DefaultParams().FreshnessWindow` = 900 sn **ve** imzalı
> bağlamın TTL'i = 900 sn, yani bandın genişliği sıfırdı.
>
> **1. "POST en yeni GET kaydına bakar" — `tapcontext.go` (73-79) `EARLIEST`
> yazıyor.** İki cümle çelişiyordu; ikisi de artık ölü, çünkü tablo yapılmadı
> (aşağıya bak). Kaydı düzeltmek için: kartın gerekçesi (saldırgan gelecek
> sayaçlar için eski damgalı satır üretip meşru kullanıcıyı reject'e düşürür)
> **en eski**'yi ister, çünkü **en yeni**'yi seçmek saldırganın taze bir satır
> ekleyip pencereyi kendi lehine sıfırlamasına izin verir.
>
> **2. 🔴 "Sıra senaryosu korunuyor: A dokunur, B dokunur, A basar → A hâlâ
> geçerli" — BU KRİTER BUGÜN YANLIŞ ve hiçbir tazelik tasarımıyla
> karşılanamaz.** Ölçüldü (gerçek Postgres, üretim yolu, `last_ctr` 700'den
> başlıyor):
>
> ```
> B(ctr=702) → 200  verdict=flag    sid=base:ctr-gap-review   last_ctr=702
> A(ctr=701) → 200  verdict=reject  sid=sys:sun-invalid       last_ctr=702
> ```
>
> A, kartın reddettiği `seen_ctr` tasarımı yüzünden değil, **§4.4'ün monoton
> `ctr`'ı** yüzünden reddediliyor: sayaç 702'ye ilerledikten sonra 701 tanım
> gereği replay'dir. §5'in *"ardışık farklı kişiler hepsi `ok`"* gereksinimi
> **debounce** hakkındadır (kişi bazlı, `sys:person-debounce`), aynı plaketin
> `ctr` sırası hakkında değil — kart ikisini karıştırmış. Kartın `seen_ctr`
> reddi **yine de doğru** (GET'te sayaç harcamak sayfayı açıp basmayanı cezalandırır,
> M5-04); düzelen yalnız **gerekçesi**.
>
> **3. `tap_page_views` TABLOSU YAPILMADI — kullanıcı kararı (2026-08-02).**
> **Bu bir ARGÜMANDIR, ölçüm değil** — yapılmamış bir tablonun katkısı
> ölçülemez; ilk yazımı "ölçüldü" diyordu ve bu yanlıştı. Argüman: pencere zaten
> `GET` anından ölçülüyor (imzalı `IssuedAt`, sunucu saati) ve saldırgan ilk
> GET'in **zamanını kendisi seçtiği** için "en eski satırı pinlemek" tehdit
> modelinde bir şey kazandırmıyor. Argümanı destekleyen **iki ölçülmüş olgu**
> aşağıda: pencerenin `GET` anından ölçülmesi (`IssuedAt` MAC'in içinde,
> `PageIssuedAt` oradan besleniyor — silinince **üç** DB testi kırmızı) ve
> oturumsuz `GET /t`'nin 303'te durması.
> Tablonun tek gerçek katkısı M6-11'in *"POST'suz `GET /t`
> sayısı"* metriğiydi; o **M6-11'e devredildi** (kartın kendi ifadesiyle
> *"ikincil sinyal, uçak modu senaryosunda sıfır kalır"* — asıl sinyal olan
> `ctr` boşlukları zaten çalışıyor).
> Kartın *"kimliksiz herkes `GET /t` açabildiği için temizliksiz tablo ucuz bir
> disk doldurma vektörü"* gerekçesi de **yanlıştı**: oturumsuz `GET /t`
> **303'te duruyor**, ön-kontrole hiç ulaşmıyor (ölçüldü: `303 → /activate`,
> `transactions` 2→2, `last_ctr` 702→702). Gerçek tavan oturum başına
> **300 GET/10 dk**'dır (M5-03 bütçesi).
>
> **4. "Sayaç boşluğu hesaplanıp `base:ctr-gap-review`'a besleniyor" — ZATEN
> TAMAMDI** (M5-05). Yukarıdaki ölçümde `sid=base:ctr-gap-review` canlı ateşledi.
> Kartta kalması M5-10'un başkasının işini kendine yazması olurdu.
>
> **5. "süre tenant tarafından 1–15 dk arası seçilir" — bugün TENANT BAŞINA
> DEĞİL, env.** Hiçbir bounded param tenant başına saklanmıyor (`tenants`'ta ayar
> sütunu yok; `policySets.params` süreç genelinde tek). Emsal `TAPPA_GPS_RADIUS_M`
> ve `TAPPA_DEBOUNCE_SECONDS`'tır ve M5-10 aynı kalıbı izledi. **Tenant başına
> ayar M6-09'un işidir**; M5-10 ilk tenant-başına parametre depolamasını
> **açmadı**.
>
> **6. "Neden yine de gerekli … saatler sonra basan kullanıcıyı durdurur" —
> bunu 15 dk'lık TTL zaten durduruyordu.** Doğru gerekçe iki tanedir:
> (a) cevabın **türü** — kayıtsız 400 yerine **kayıtlı reject** (§4.6);
> (b) **sıkılık** — 900 sn yerine 180 sn.
>
> **7. 🔴 SAYILAN LİMİT — 180–900 sn kayıtlı reject, >900 sn KAYITSIZ 400.**
> `tapContextTTL` **15 dk kalıyor** (kullanıcı kararı, 2026-08-02). Yani bugün
> üç bant var:
>
> | Sayfa yaşı | Cevap | Kayıt |
> |---|---|---|
> | ≤ 180 sn | normal tap | var |
> | 180–900 sn | `reject` / `sys:tap-freshness` | **var** (§4.6) |
> | > 900 sn | 400 "tekrar dokun" | **yok** |
>
> Üçüncü bant §4.6 deliği **değil** — ama gerekçesi *"kayıt yazılamazdı"*
> **DEĞİL**; ilk yazımda öyle diyordu ve **ölçümle yanlışlandı**. Doğrusu:
> TTL aşılınca **TAP** doğrulanmamış olur (tag, sayaç, kanal, lokasyon), fakat
> **OTURUM doğrulanmıştır** — `handler/checkin.go` kimliği `httpx.IdentityOf`
> ile çözüp `SessionLive` şartını koyduktan **sonra** bağlamı parse ediyor, yani
> reddin olduğu anda `id.Session.TenantID` **ve** `id.Session.EmployeeID` elde.
> Şema da izin veriyor: `00005`'te `tag_uid`/`employee_id`/`location_id`/`ctr`
> **NULLABLE** ve ilk CHECK `verdict IN ('reject','ignored')` için `employee_id`
> NULL'a açıkça izin veriyor. **Yani satır yazılabilirdi ve atfedilebilirdi.**
> Yazmamak bir **kullanıcı kararıdır** (2026-08-02) ve gerekçesi şu: 400
> **sessiz değil** ("tekrar dokun"), çalışan zaten plaketin önünde ve o anda
> **kaydedilecek bir mesai olayı yok** — satır kişiyi adlandırır ama plaketi
> adlandıramaz, yani bir geliş/gidiş değil "bayat sayfa POST edildi" bilgisini
> kaydederdi. Pin: `TestCheckinDB_TwoCeilingsBoundATapPageAndTheyAnswerDifferently`.
>
> **7b. ⚠️ `TAPPA_FRESHNESS_SECONDS=900` KABUL EDİLEN, SESSİZ BİR NO-OP.**
> Aralığın üst ucu TTL ile **aynı sayı** olduğundan, 900 yazan bir kurulumda
> kayıtlı-reject bandı **tam olarak boşalır** (guardrail `age > 900` arıyor,
> parse zaten `age > 900`'ü önce reddediyor) — yani M5-10 öncesi atıl duruma
> dönülür ve **hiçbir başlangıç uyarısı verilmez**. Ölçüldü: harness penceresi
> 900 iken 870 sn'lik sayfa → `200`, `verdict=ok`, `sid=base:ip-or-gps-ok`
> (180/240 sn'de aynı istek `reject` / `sys:tap-freshness`). Değer **kabul
> edilmeye devam ediyor** (ADR 0004 §11 aralığının gerçek bir noktası,
> `TestLoad_FreshnessRange` iki ucu da kapalı olarak pinliyor) ama sonucu artık
> `.env.example`, `config.Config.Freshness` ve `policy.FreshnessMaxSeconds`
> üzerinde **yazılı**. Kayıtlı bant isteyen kurulum 900'ün **altında** kalmalı.
> Alt uç böyle bir tuzak taşımıyor; yalnızca sıkı ve kartın *"< 1 dk meşru
> kullanıcıyı düşürür"* uyarısı orada geçerli.
>
> **8. "Tuzak: `first_seen_at` sunucu saatidir; istemciden gelen zamana asla
> güvenilmez" — bu tuzak M5-04'te ZATEN KAPANDI.** `tapcontext.go` `mint`
> damgayı `c.clock()`'tan alıyor, damga payload'ın 8. alanı ve **MAC'in içinde**;
> `handler/checkin.go` onu form alanından değil **MAC'ten** okuyor. Kartın
> M5-04'ten önce yazıldığının en net kanıtı bu satır.
>
> **9. A1 KAPANMADI ve kapandığı iddia edilmiyor** (kartın kendi uyarısı geçerli).
> Pencere `GET` anından başlıyor, o anı saldırgan seçiyor: uçak modunda
> biriktirilen URL ertesi gün açılır, sayfa **o an** mintlenir, 5 sn sonra POST
> pencere içindedir. ADR 0005 Risk 3'te **kabul edilen risk**; tek gerçek iz
> `tap:ctrGap`.
>
> **Kanıt (hepsi `make test` ile, gerçek Postgres).**
> - `TestCheckinDB_ConfiguredFreshnessWindowReachesTheGuardrail` — 300 sn'lik
>   sayfa → 200, satır **+1**, `verdict='reject'`, `matched_sid='sys:tap-freshness'`,
>   `policy_context.tap:pageAgeSeconds` ≥ pencere, `last_ctr` **+1**.
>   Pencere **config'ten** sürülüyor; harness 240 sn (varsayılan 180 ve fallback
>   900'ün **ikisi de değil**), yani `params.FreshnessWindow = cfg.Freshness`
>   satırı silinince test **kırmızı** (ölçüldü: 1,39 sn, iki test birden).
>   ⚠️ 300 sn'lik sayfanın **ne olduğu** (2. denetim turunda düzeltildi — burada
>   ve `tap_db_test.go`'da *"her iki fallback'te de sıradan bir tap"* yazıyordu,
>   **aritmetik olarak yanlış**): 300 sn, 240'ta da **180'de de** KAYITLI
>   reject'tir; sıradan tap **yalnız 900** fallback'inde. Ölçüldü: harness geçici
>   olarak 180'e çekilip ikinci nöbetçi susturulunca alt test
>   *"past the window the tap is denied AND recorded"* **YEŞİL** kaldı.
> - `TestCheckinDB_StaleQRPageIsNotDeniedByFreshness` — aynı yaşta **QR** tap'i
>   `ok`; guardrail NFC-only. Mutasyon (kanal kontrolünü sil) → KIRMIZI.
>   🔴 Bu ölçüm **QR hakkındaki üç yazılı vaadi yanlışladı** ve üçü de düzeltildi
>   (`qr_db_test.go` ×2, `tap/tenant_occurredat_test.go` ×1): M5-10 QR tavanını
>   **değiştirmiyor** — ne "onunla birlikte küçülüyor" ne de "yarıya iniyor".
>   QR frenleri `base:qr-requires-ip` + 60 sn kişi-debounce (fazlaları **kayıtlı
>   `ignored`**) + oturum/adres limitleri olarak kalıyor.
> - `TestLoad_FreshnessRange` — sınır değerlerin **ikisi de kabul** (60 ve 900,
>   aralık kapalı); 59, 901, 3600, 0, -1 ve sayı olmayan değer **başlangıç
>   hatası**. Üst sınır vakasının adı artık **ne pinlediğini söylüyor**
>   (md. 7b'deki sessiz no-op).
> - `TestNew_RefusesAZeroFreshnessWindow` (yeni, DB'siz) — **tehlikeli sıfır
>   değer kapatıldı.** Ölçüldü (düzeltmeden önce, harici paketten):
>   `checkin.New(…, &config.Config{/* Freshness yok */}, …)` → `err=nil`, etkin
>   pencere **15m0s**, yani guardrail atıl. **İki çağıran bunu yapıyordu**:
>   `seedflow_db_test.go` (yani `make simulate-day`) ve
>   `internal/domain/checkin/advance_test.go`. Karar: `checkin.New` artık sıfır
>   `Freshness`'i **reddediyor** (M5-01 kalıbı) ve iki çağıran da değer veriyor —
>   `seedFreshness = policy.FreshnessMinSeconds` (gün, sayfa yaşı ~ms olduğu için
>   en sıkı yasal pencerede koşuyor), `testConfig()` 120 sn. `Debounce`'ta bu
>   asimetri **yok** ve olmamalı: onun fallback'i (60) sevk edilen varsayılana
>   **eşit**, tazelikte fallback (900) hem farklı hem **en gevşek** değerdi.
>   Aynı test wiring'i **DB'siz** de yanlışlanabilir kılıyor: `testConfig()`'in
>   120'si `DefaultParams()`'ın **900**'ünden farklı olduğu için atama silinince
>   `FreshnessWindow = 15m0s` → KIRMIZI. ⚠️ *"120 ≠ **180** ≠ 900"* diyen ilk
>   yazım **fazla-belirtimdi** (3. denetim turu ölçtü, düzeltildi): `checkin.New`
>   hiç `config.Load` çağırmıyor, yani 180 bu testte kaza eseri düşülebilecek bir
>   değer değil. Ölçüldü: `testConfig()` **180** yapılıp atama da silindiğinde
>   test **yine KIRMIZI** (`15m0s ≠ 3m0s`). Yük taşıyan tek özellik **≠ 900**.
>   ⚠️ **Sıfır kontrolünün kendisi tek başına yanlışlanamazdı** ve bu da 3. turda
>   ölçüldü: atama **koşulsuz** olduğu için sıfır her hâlükârda
>   `params.Validate()`'e ulaşıp orada reddediliyor, dolayısıyla `if cfg.Freshness
>   <= 0` bloğu **silindiğinde** `checkin`+`config`+`handler` **yeşil** kalıyordu.
>   Yani deliği kapatan şey **koşulsuz atama + `Validate`**; blok yalnız **mesajı**
>   iyileştiriyor (`Validate` "0s aralık dışı" der ve okuru olmayan bir politika
>   hatasına yollar). Seçilen çözüm: test artık hata **metnini** de doğruluyor
>   (`"config Freshness is not positive"` — 4. turda "is zero"dan düzeltildi, bkz.
>   aşağıdaki blok), böylece blok gerçekten yanlışlanabilir —
>   ölçüldü, blok silinince `checkin` **KIRMIZI**
>   (`checkin: policy: freshness window = 0s is outside [60, 900] seconds`).
> - `TestDefaultParams_FreshnessStaysAtTheRangeMaximum` (yeni) — *"fallback sevk
>   edilen varsayılana eşit olmamalı"* özelliği artık **düzyazıda değil, testte**.
>   Denetim turu ölçtü (düzeltmeden önce): `DefaultParams().FreshnessWindow`
>   900→180 **tek başına** → 13 paket **yeşil**; ardından wiring de silinince →
>   **hâlâ yeşil**. Sebep doğrulandı: iki nöbetçi de `harnessFreshness`'i (240)
>   karşılaştırıyor — biri `DefaultParams()` ile (180≠240), diğeri 180 ile
>   (240≠180) — yani mutasyonda **ikisi de ateşlenemez**. Bu turda ölçülen:
>   düzeltmeden sonra aynı mutasyon → **KIRMIZI** (`policy`, 0,52 sn).
> - `TestGuardrails_FreshnessBoundaryIsStrictlyGreater` (yeni) — sınır artık
>   pinli: `age == pencere` **taze**, `age > pencere` reddediliyor. Mutasyon
>   (`>` → `>=`) → **KIRMIZI**. Gerekçe: `TestLoad_FreshnessRange` config
>   sınırlarını bilerek kapalı pinliyor, guardrail sınırının operatörden
>   çıkarılmaya bırakılması asimetriydi.
> - **Sayaç davranışı (§4.4):** tazelik reddinde `last_ctr` **ilerliyor** ve bu
>   doğrudur — advance, `checkin.Record`'un 2. adımı, her karardan önce. Çip o
>   `(uid, ctr)` çiftini gerçekten yaydı; değer **harcanır**, böylece aynı bayat
>   URL ikinci kez sunulduğunda ikinci bir tazelik reddi değil **replay**
>   (`sys:sun-invalid`) olur. İlerletmemek çifti ileride kullanılmak üzere canlı
>   bırakırdı — tam da bu pencerenin daraltmaya çalıştığı biriktirme.
>
> **Ölçüm (3. tur, `.env` yüklü).** `make test` → **13 paket ok**, 129,5 sn,
> **1272 PASS / 0 SKIP / 0 FAIL**. Sayım komutu (tek yöntem olsun diye yazılı):
> `make test GOFLAGS=-v` çıktısında `grep -c -- '--- PASS:'` (alt testler dâhil).
> ⚠️ **Çıplak `go test` ile sayma:** `.env` yüklenmez, 13 paketin DB'ye bağlanan
> testleri atlanır ve sayım **1022 PASS / 176 SKIP**'e düşer — denetim turunda
> birebir yaşandı.
>
> **Üretim ayak izi — 11 yorum-dışı satır.** `internal/config/config.go` **6**
> (`Freshness` alanı + `floatEnvRange` bloğu) ve
> `internal/domain/checkin/checkin.go` **5** (sıfır kontrolü 4 + atama 1).
> Değişen diğer üç üretim dosyası (`policy/guardrails.go`, `handler/tapcontext.go`,
> `domain/tap/types.go`) **yalnız yorum**. ⚠️ Daha önce yazılan **9** sayısı
> `checkin.New`'in sıfır kontrolünden **öncesine** aitti. **4. turda değişti:**
> `policy/guardrails.go` artık yorum-dışı da değişiyor (guardrail sırası) ve
> `checkin.go`'nun sıfır kontrolü `fmt.Errorf`'e döndü — bkz. aşağıdaki blok.
>
> > ⚠️ **YÜRÜRLÜKTEKİ SAYI 11 DEĞİL (yeniden ölçüldü 2026-08-02, 5. tur).**
> > Yukarıdaki 11, yukarıda kendi kabul ettiği iki değişikliği **saymıyor**.
> > Doğru ayrım iki kalemdir, çünkü biri yeni mantık diğeri yer değiştirme:
> >
> > - **13 yorum-dışı EKLENEN satır, 0 silinen** — `internal/config/config.go`
> >   **6** (`Freshness` alanı + `floatEnvRange` bloğu), `internal/domain/checkin/checkin.go`
> >   **7** (`fmt.Errorf`'e dönen sıfır kontrolü 6 + atama 1; 4. turdan önce 5'ti).
> > - **18 satır YER DEĞİŞTİRDİ** — `internal/policy/guardrails.go`'da guardrail
> >   sırası (ADR 0007). Eklenen ve silinen yorum-dışı satır kümeleri **birebir
> >   aynı** (`sort | diff` → fark yok), yani **net yeni mantık sıfır**; değişen
> >   şey yalnız blokların sırası — ki bu diff'teki asıl güvenlik değişikliği odur.
> >
> > `domain/tap/decide.go`, `domain/tap/types.go` ve `handler/tapcontext.go`
> > yorum-dışı **0/0**: yalnız yorum. Komut:
> > ```
> > git diff -U0 -- <üretim dosyaları> | grep '^+' | grep -v '^+++' \
> >   | sed 's/^+//;s/^[[:space:]]*//' | grep -v '^$' | grep -v '^//' | wc -l
> > ```
> > (silinenler için `'^-'` / `'^---'`; yer değiştirme kanıtı için iki kümeyi
> > `sort` edip `diff`le.)

> ### 🔴 4. tur — `tappa-security-auditor` REGRESYON buldu (2026-08-02)
>
> Genel üçüncü göz iki turda kaçırdı; güvenlik denetçisi ölçtü. **Bu diff'in
> ürettiği gerçek bir regresyon:** M5-10 bandı açınca `sys:tap-freshness`
> (o sıradaki #4) `sys:employee-deactivated`'i (#7) **ön-almaya başladı** ve
> §5 satır 4'ün *güvenlik uyarısı* düştü — ret ve kayıt yerinde kaldığı için
> satır doğru görünüyordu.
>
> ```
> window=15m0s  DEACTIVATED, 5 dk bekledi -> sid=sys:employee-deactivated  ALERTS +1  (öncesi)
> window=3m0s   DEACTIVATED, 5 dk bekledi -> sid=sys:tap-freshness         ALERTS +0  (sonrası)
> ```
>
> **Aile taraması bir ikinci yol buldu, ve o M5-10'dan eskiydi:**
> `sys:occurred-at-bound` (#5) de uyarıyı düşürüyordu ve girdisi bir **POST form
> alanı** — deaktif oturum `occurred_at = now + 60 sn` beyan ederek uyarıyı
> kendisi kapatabiliyordu. İkisi tek düzeltmeyle kapandı: guardrail sırası
> `… sun-invalid(3) · no-session(4) · employee-deactivated(5) · tap-freshness(6) ·
> occurred-at-bound(7) · person-debounce(8) …`. §5'in beş satırı artık kendi
> sırasında — konumları `[2 3 4 5 8]`. ⚠️ **KESİNTİSİZ DEĞİL:** iki zamanlama
> kuralı §5 satır 4 ile satır 5'in **arasında** duruyor. Ve eski sıra da §5'in
> satırlarını artan konumlarda tutuyordu (`[2 3 6 7 8]`), yani "göreli sıra
> korundu" ölçütünü regresyonu üreten sıra **da** geçiyordu. Kazanılan şey §5
> uygunluğu **değil**, **uyarıdır**.
> **`sys:sun-invalid`'in ön-alması KALDI** (sahte SUN uyarı imal edememeli, R8) —
> gerekçe, mutasyonlar ve reddedilen alternatifler
> [ADR 0007](../adr/0007-guardrail-sirasi-ve-guvenlik-uyarisi.md)'de.
>
> **Yeni pinler:** `TestGuardrails_TimingRulesDoNotPreemptTheDeactivatedAlert`
> (policy, 4 vaka + sahte-SUN karşı-örneği) · `TestDecide_DelegatesOrderToPolicy`
> içindeki iki ikiz vaka (`freshness_does_NOT_…`, `occurred_at_bound_does_NOT_…`) ·
> `TestCheckinDB_DeactivatedAlertSurvivesTheTimingGuardrails` (gerçek Postgres,
> `tap.security_alert` satırını **sayar**). Mutasyon: her iki sıra değişikliği
> tek tek geri alındığında üç katman da **KIRMIZI** (4–11 sn); ikisinde de
> `sun_invalid_preempts_deactivated_alert` **yeşil kaldı**, yani testler
> ön-almanın kendisini değil hangisinin meşru olduğunu ölçüyor.
>
> **⚠️ 3. turun "config Freshness is zero" pini değişti.** Guard `<= 0` ve
> **negatif değer erişilebilir**: `TAPPA_FRESHNESS_SECONDS=NaN` config'in aralık
> kontrolünden geçiyor (her NaN karşılaştırması false) ve
> `time.Duration(NaN × 1sn)` int64 **minimumu** oluyor (`-2562047h47m16.85s`).
> Metin artık `"config Freshness is not positive (%v)"` ve değeri basıyor;
> `TestNew_RefusesAZeroFreshnessWindow` iki şekli de (0 ve negatif) sürüyor.
> Mutasyon (eski metne dön) → **KIRMIZI**, 6 sn.
>
> **🟡 DEVREDİLEN — `floatEnvRange` NaN'ı geçiriyor.** Ölçüldü, üç çağıran:
> `TAPPA_GPS_RADIUS_M=NaN` → `Load` ve `checkin.New` **err=nil**, yarıçap NaN,
> her mesafe karşılaştırması false → GPS eşleşmiyor (**daraltıcı**, §4.6 sağlam);
> `TAPPA_DEBOUNCE_SECONDS=NaN` → negatif süre → `cfg.Debounce > 0` false →
> sessizce 60 sn fallback; `TAPPA_FRESHNESS_SECONDS=NaN` → aynı negatif süre,
> ama `checkin.New` **yakalıyor** (tek fail-closed olan). Yani üçünden ikisi alt
> katmanın kazasına bağlı. Tek satırlık düzeltme (`math.IsNaN(v)` reddi) üçünü
> birden kapatır; **M5-10 kapsamı dışı**, bu yüzden `config.floatEnvRange`'in
> üstüne ölçümüyle birlikte **SINIR olarak yazıldı**, düzeltilmedi.

---

## M5-11 — Practice satırı açık girişi maskeliyor (§5 yön kuralı ihlali)

- **Bağımlılık:** M5-05 · M5-07 (practice) · M5-09 (bulan ölçüm)
- **Kırmızı çizgi:** §5 yön kuralı · §4.6 (kayıt kaybolmuyor ama **kapanmıyor**)
- **Commit:** `fix(tap): let direction skip practice rows and find the real open entry`

> **Neden bu kart var.** M5-09'un yapıcısı buldu, iki denetçi doğruladı ve
> büyüttü, **kullanıcı 2026-08-02'de "şimdi düzelt, kendi görevi olsun" dedi.**
> M5-09 bir test görevi olduğu için orada düzeltilmedi; gün fixture'ı bunun
> **etrafından dolaşıyor** (Ivan'ın practice tap'i 17:50 beyan ediliyor).

**Belirti (ölçüldü, üç bağımsız sonda).** Bir `practice=true` satırı, kendisinden
**daha eski** ve hâlâ **açık** bir gerçek girişi maskeliyor:

| kol | practice'in `occurred_at`'i | sonraki tap'in yönü | kalan açık giriş |
|---|---|---|---|
| kontrol | gerçek girişten **eski** | `out` ✅ | **0** |
| bozuk | gerçek girişten **yeni** | `in` 🔴 | **2** |

Üçüncü sonda M5-09'un gün testinin **kendisi**: Ivan'ın practice tap'inden
`declaring(...)` workaround'u kaldırılınca gece vardiyası kapanmıyor —
`day_db_test.go:546`, *"Ivan's 02:10 tap = in, want out"*.

**Mekanizma.** `GetLastOpenTransaction` **tek** satır döndürüyor
(`ORDER BY occurred_at DESC LIMIT 1`, practice'e **kör**), tüketici
(`internal/domain/checkin/checkin.go` ≈937, `if !open.Practice`) dönen satır
practice ise onu **atıyor ve bir alttakine bakmıyor**. Yani "son açık giriş"
sorusu, "son açık giriş **practice değilse**" diye cevaplanıyor.

**İhlal edilen cümle (CLAUDE.md §5).** *"Yön (in/out): kişinin **son açık
girişine** göre toggle."* Sonuç: çıkış `in` olarak yazılıyor, gerçek giriş
**hiç kapanmıyor** ve müdüre §5'in *"unutulmuş çıkış"* anomalisi gibi görünüyor.
**Hiçbir sinyal yok** — verdict `ok`, not yok, flag yok.

**🔴 Düz HTTP'den erişilebilir.** Kötü niyet gerekmiyor: `occurred_at` sevk
edilmiş bir form alanı ve `sys:occurred-at-bound` tavanı **72 saat**
(`policy.OccurredAtSkewMaxSeconds`, ADR 0004 §11). Sıradan bir practice tap +
geriye tarihli tek bir giriş yeter. **M9-01 çevrimdışı kuyruğu** tam olarak bu
şekli üretir.

**Gerçek hayat senaryosu (bu yüzden teorik değil).** Çalışan giriş yapar →
telefonunu kaybeder → yeni cihazda **yeniden aktive olur** → çıkış için dokunur.
Aktivasyon sonrası ilk kayıt tanımı gereği `practice=true`. Açık girişi kapanmaz,
saatleri eksik kalır. Bu zincirin gerçekten böyle davrandığı **ölçülmeli** —
`practice` türetiminin "hiç kaydı yok" mu yoksa "bu aktivasyondan sonra ilk" mi
olduğu bu senaryonun erişilebilirliğini belirler.

**Tasarım — üç seçenek, biri seçilip gerekçelendirilecek.**
1. **Sorguda ele:** `GetLastOpenTransaction`'a `AND practice = false` ekle. En
   basit; ama "açık giriş" tanımını sorguya gömer ve practice satırının kendisi
   hiç açık sayılmaz — bugünkü tüketici davranışıyla aynı, farkı **altına
   bakabilmesi**. Yeni sorgu = `db/queries/*.sql` + `make sqlc`.
2. **Sorgu N satır döndürsün**, tüketici practice'leri atlayarak ilk gerçek açık
   girişi bulsun. Daha esnek, ama sınırsız tarama riski (§4.3 satırları kalıcı).
3. **Semantiği değiştir:** practice satırı hiçbir zaman `type` taşımasın (yön
   üretmesin) — o zaman "açık giriş" sorgusu onu zaten görmez. En temiz olabilir
   ama **M5-07/M4-06 semantiğine dokunur** ve mevcut satırlar geriye dönük
   değişmez (§4.3).

**Bu bir semantik karardır → ADR yaz** (CLAUDE.md §10: karar motoru değişti).
ADR şunu da cevaplamalı: *bugün yazılmış, practice yüzünden kapanmamış açık
kayıtlara ne olacak?* (§4.3 gereği düzeltme = **yeni kayıt + `audit_log`**,
UPDATE değil.)

**Kabul kriterleri.**
- Yukarıdaki **kontrol/bozuk** tablosu düzeltme sonrası **iki kolda da** `out` /
  `0 açık giriş` veriyor — ve bu bir testle pinli.
- M5-09'un gün testindeki **workaround kaldırılıyor** (Ivan'ın practice tap'i
  artık `declaring(...)` olmadan da doğru sonucu vermeli). Kaldırılmazsa görev
  bitmemiştir: fixture kusuru gizlemek için oradaydı.
- `practice` satırı hâlâ **saate sayılmıyor** ve TRAINING damgası korunuyor
  (M5-07 kazanımı geriye gitmesin — mutasyonla kanıtla).
- **Ardışık practice** durumu: iki practice arka arkaya gelirse (yeniden
  aktivasyon) yön yine gerçek açık girişe göre çözülüyor.
- Seçilen tasarımın **taranan satır sayısı** ölçülü (`EXPLAIN (ANALYZE, BUFFERS)`)
  — §4.3 yüzünden kişinin geçmişi yalnız **büyür**.
- `-race` temiz · `make check` yeşil · ADR yazıldı.

**Tuzaklar.**
- **`transactions` immutable.** Var olan satırları düzeltme; davranışı ileriye
  dönük düzelt ve geçmişe ne olacağını ADR'de yaz.
- Düzeltme **açık kayıt sayısını** değiştirir → M6-07/M6-11'in "unutulmuş çıkış"
  anomali listesi bundan etkilenir; o kartlara not düş.
- Bu bulgu M5-09'un `LIMITS` bölümünde (L3) ve M5-09 kart düzeltmesi md. 6'da
  yazılı. **Görev bitince ikisini de kapat** — yoksa repoda kapanmış bir kusuru
  açık gösteren iki cümle kalır.
