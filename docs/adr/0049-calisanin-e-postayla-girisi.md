# ADR 0049 — Çalışanın iş e-postasıyla girişi

- **Durum:** Önerildi (2026-11-18; aynı gün iki plan denetiminin bulgularıyla yeniden yazıldı) — kabul M16-34'te,
  kullanıcının. Bu belge kullanıcının **bir** açık kararını ve ana oturumun **bir** kararını kaydeder (aşağıda); geri kalan
  her şey **öneridir** ve Q44, Q45, Q49 cevaplanmadan kabul edilmez (M16-34).
- **Tarih:** 2026-11-18 · **Dal:** `plan/m16-mobil` (yalnız belge) · **Plan:** [docs/plan/m16-mobil.md](../plan/m16-mobil.md)
  → *Faz G* (M16-34, M16-35, M16-36)
- **Karar veren:** kullanıcı (ürün sahibi), 2026-11-18, AskUserQuestion: giriş *"sign in with your work email"*dir — çalışan
  iş e-postasını yazar, e-postayla tek kullanımlık bir link alır, cihaz bağı işverenin plaketine **ilk gerçek NFC
  dokunuşunda** tamamlanır; **kendi kendine kayıt yok**; yalnız **kayıtlı e-postası olan aktif** çalışanlar; oran sınırlı;
  **numaralandırma yok**. **Ana oturum kararı** (2026-11-18, denetim H3): uç **varsayılan kapalıdır** ve önce yalnız demo
  işletmede, kullanıcının onayladığı ayrı bir config PR'ıyla açılır (§2.7).
- **İlgili:** ADR 0002 md. 7 (`tappa_resolver`) · ADR 0005 (kabul edilen riskler) · ADR 0010 (deaktivasyon) · ADR 0022
  (§6 istek yolu dışı gönderim, §7 davet, §8 şablon, §9 oran, §10 log) · ADR 0025 (askı) · ADR 0026 (aktivasyon) · ADR 0028
  §6 (K-19) · ADR 0031 (panel kimliği) · ADR 0045 (§2.3 cihaz anahtarı, Q40) · CLAUDE.md §4.4, §4.5, §5 *"Aktivasyon
  dokunuşu"*, §6, §7 · open-questions Q11, Q13, Q40, Q44, Q45, Q49.
- **Atıf kuralı:** `file:line` atıfları `origin/main` `6ab423eb`'ye göredir. **Okundu** = dosyadan okundu, çalıştırılmadı.

---

## 1. Bağlam — bugün (okundu)

1. **Aktivasyonu yalnız bir davet başlatır ve daveti yalnız işveren tarafı basar.** `invite.Manager.IssueAndDeliver`
   davet yaratmanın *"ONLY exported way"*idir (`internal/invite/manager.go:218-220`); çağıranları panelin müdür eylemi ve
   M11'in K-17 kuyruğudur (`internal/invite/queue.go:3-6`). Yeni telefon, silinen çerez (Q11; 2026-11-17 *"Finish setup"*
   vakası) ya da yeniden kurulan uygulama → müdürden yeniden davet.
2. **Davet e-postayla gider** — üretim `TAPPA_INVITE_DELIVERY: "email"` (`deploy/k8s/05-config.yaml:166`). Aktivasyon linki
   bugün de kayıtlı posta kutusuna düşer; değişen **kimin bastığıdır**. Bu yüzden `main`'e giren her davet ucu **üretimde,
   her işletmede** açılır (birleştirme = deploy) — §2.7'nin gerekçesi.
3. **Davetin kuralları:** kod **256 bit** (`internal/invite/code.go:55`, `codeBytes = 32`), yalnız HMAC'i saklanır (`00009`),
   tek kullanımlık; basım kişinin öteki bekleyen davetlerini aynı transaction'da emekliye ayırır (`manager.go:237-254`,
   `:312-318`); ADR 0022 §9 sınırları (`internal/invite/email.go:75-79`: işletme 50/sa, 300/gün; kişi 3/sa), posta kutusu
   tavanı (EM-7C), süreç kesicisi (`internal/mail/breaker.go:124-126`: `BreakerLimit = 300` / saat, **bütün** gönderimler);
   yönetici adresine eşit adres reddi (`ErrAddressIsAdministrators`, `email.go:100`). Süre `minTTL` 1 sa … `maxTTL` 30 gün,
   varsayılan 7 gün (`manager.go:133-135`). `Lookup` süresi dolmuş kodu reddeder (`manager.go:493-494`).
   **Davet yolunda askı kontrolü yoktur** — askıdaki işletme bugün davet basabilir (denetçinin ölçümü: panel `dashboard.go`
   ve `/v1`'in `api/suspension.go` kapıları davet basımını kapatmaz).
4. **K-17 kuyruğu bu iş için uygun değildir:** işçi, API kapısı kapalı işletmede basmaz (`queue.go:301-313`: *closed*,
   `queueClosedDelay` 15 dk, `queueLifetime` 72 sa — `:79-83`) ve iş K-19'un süreç içi *"işi olan tenant'lar"* kümesine
   bağlıdır (ADR 0028 §6; `internal/webhook/worker.go:95` `Notice`) — pod yeniden başlayınca bekleyen iş, o tenant'ta
   bir sonraki olaya kadar uyur.
5. **Tamamlanma ADR 0026'nındır** (uygulamada aynı sıra, ADR 0045 §2.3): onay + bağ → ilk gerçek NFC → ilerletmeyen ön
   kontrol (işverenin **herhangi bir** aktif, monte plaketi) → `sun.Verify` → tek ifadeli tüketim → oturum/cihaz; kişinin
   öteki kimlik bilgileri aynı transaction'da iptal (Q40 (a); `internal/handler/appactivate.go:24-27`).
6. **`employees.email` tenant içinde tekildir, global değil, boş olabilir** (`00003:48-53`, `:83-84`); tek adres indeksi
   `(tenant_id, email)`'dir. Sabit `search_path`'li bağlamda `citext` eşitliği sessizce düz metne düşer (`00011:295-313`).
   Bugün adresle çalışan arayan tek sorgu `FindLinkableEmployeeByEmail`'dir (`db/queries/employees.sql:1117`) ve
   **bir işletmenin içinde** arar (*"NEVER across tenants"*); işletmeler arası arama yoktur. `tappa_resolver`'ın `employees`
   üzerinde yetkisi yoktur.
7. **Kimliksiz, adresle başlayan isteğin emsali** `POST /admin/reset`'tir: her durumda aynı cevap, 250 ms taban
   (`internal/handler/adminresetlimits.go:347`; `adminreset.go:1640-1655`), basım istek içinde, gönderim süreç içi sınırlı
   bir çıkış kutusundan (`internal/handler/adminresetoutbox.go`, ADR 0022 §6), adres log'a yazılmaz (ADR 0022 §10).
8. **Neden şimdi:** App Store Review Guidelines 2.1(a) giriş içeren uygulama için inceleme hesabı ister; daveti olmayana yol
   göstermeyen giriş ekranı ret riskidir. Uygulama hesap yaratmaz (3.1.3(c)); 4.8 gereği Sign in with Apple gerekmez.

## 2. Karar (öneri; iki karar yukarıda)

### 2.1 Kim, ne zaman alır

Sırayla; her ret **sessizdir** (§2.2'nin tek cevabı, basım yok):

1. Adres env listesindeki (§2.7) bir işletmenin **aktif** (`status='active'`) ve adresi dolu bir çalışanına ait (çözümleyici;
   `ORDER BY tenant_id, id LIMIT 5` — beşten fazlası sayılı sınır).
2. Eşleşmenin kendi tenant bağlamında: işletme **askıda değil** (L4 kararı — davet yolunun bugünkü boşluğu bu yolda kapanır);
   Q49 (a) ise işletmenin `email_signin` ayarı açık; adres aynı işletmede bir yönetici adresi değil.
3. **İşverenin bastığı canlı bir davet yok** (`employee_invites.origin = 'employer'`, kullanılmamış, iptal edilmemiş, süresi
   dolmamış). Aksi hâlde basım onu emekliye ayırırdı ve bir yabancı müdürün 7 günlük davetini kısa ömürlü bir linkle
   değiştirebilirdi (denetim M2).
4. **Bekleme süresi:** son **7 günde** tüketilmiş bir `origin = 'self'` daveti yok (denetim H4 (2)). 7 gün: haftada bir
   telefon değişikliği hiçbir dürüst kullanımı engellemez, bir saldırganın yineleyen ele geçirmesini haftaya bir sınırlar.
5. Bütçeler (§2.4).

Aynı adres birden çok işletmede aktifse — **holding dahil** (KF + KM) — işletme başına **bir davet ve bir e-posta**; her
davet yalnız kendi işletmesinin plaketinde tamamlanır (ADR 0026; K-35). Davetli (`invited`) kişi bu yoldan bir şey almaz
(müdürün daveti zaten aynı kutudadır); deaktive kişi hiçbir şey almaz.

### 2.2 Tek cevap

`POST /app/v1/signin/request` her durumda **202**, boş gövde, `no-store`, süre **`[250 ms, 300 ms]` bandında** (reset yolunun
bandı, `adminresetoutbox.go:44-49`; M16-35 k = 0/1/5 işletme, dolu çıkış kutusu ve açık kesiciyle ölçer). Tek istisna kaynak kovası
(429 + `Retry-After`, adresten bağımsız). Eşleşmeyen adres hiçbir iz bırakmaz ve **hiç** e-posta almaz (tanımadığımız
adreslere posta göndermek bizi bir posta bombası aracı yapardı).

### 2.3 Gönderilen: taze bir davet, çıkış kutusundan

- **Ne:** `IssueAndDeliver` ile bugünkü davet; `origin = 'self'` (00116'nın yeni sütunu), **`TTL` = Q44** (öneri 24 saat;
  `minTTL`/`maxTTL` içinde, kod değişikliği yok). Kısa kod ve uygulamaya bağlı ayrı link Faz G dışıdır (*Sonra*): ilki yeni
  bir kimlik bilgisi türü, deneme kilidi ve kaba kuvvet analizi ister, ikincisi `/.well-known` yollarını ve Q40'ın kuralını
  değiştirir.
- **Nasıl gider — K-17 değil, çıkış kutusu** (§1.4), **ve kanal `*EmailChannel`'ın kendisidir** (ikinci denetim Y1):
  `IssueAndDeliver` adres kapılarını yalnız `*EmailChannel` için koşar (`internal/invite/manager.go:287` → `checkLimits`
  `:298`, `readRecipient` `:335`, `RecipientCapped` `:342`); yeni bir kanal tipi bunları atlardı. Giriş daveti bu yüzden kanal
  adı `"signin"` olan bir `*EmailChannel`'dan, sink'i **bloklamayan** bir çıkış kutusu (`signinOutboxSink`) olarak geçer. Sink
  kabul ederse işçi gönderir ve gönderimden sonra tam bir audit satırı yazar; reddederse (dolu / kapanıyor) `DeliverInvite`
  senkron `invite.undelivered` yazar. FIFO 32, tek işçi, 15 sn — `adminresetoutbox.go`'nun biçimi; kapanış dizisine reset
  çıkış kutusunun yanında girer.
- **Kurucu ve sink:** `invite.NewSigninChannel` (`channel = "signin"`); sink `internal/handler`'da (`signinOutboxSink`,
  `emailLinkSink`'in yanında), kabulde `invite.ErrQueued` döner ve `DeliverInvite` o zaman senkron satır yazmaz; `RecipientCapped`
  aynı `sender.Capped`. Giriş kanalı ret satırı (`invite.email_refused`) **yazmaz** — retler kimliksiz isteğin ürünüdür.
  `Gate`/`ExpectAddress`/`Origin="self"` `*EmailChannel` olmayan kanalla reddedilir; rota yalnız `TAPPA_INVITE_DELIVERY=email`
  iken bağlanır (panel modunun kanalı kilit almaz).
- **Bütün giriş kapıları basım transaction'ının içinde**, `LockTenantForInviteLimits`'ten sonra (`IssueParams.Gate` kancası):
  askı, Q49, işverenin canlı daveti, bekleme süresi, DB bütçeleri ve **en son** süreç alt bütçesi — `httpx.Limiter.Reserve`,
  commit'te `Keep`, geri almada `Refund` (basım sonrası bir ret alt bütçeyi yemez). Yönetici adresli çalışan çözümleyicide elenir. Kilit altında okunduğu için müdürün kapı ile basım arasında
  davet basması yarışı kapanır. `readRecipient`'tan hemen sonra adres `IssueParams.ExpectAddress`'le
  karşılaştırılır; farklıysa (owner adresi o arada düzeltti) `invite.ErrAddressChanged` ile geri alınır. Çözümleyici askıdaki ve (Q49 (a)) kapalı işletmeleri
  **önceden** süzer — kimliksiz istek başına yalnız eşleşen ve açık işletmeler için transaction açılır (taban bandı).
- API kapısı (K-22) bu yolu etkilemez: kapı **API'nin** basımını kapatır; e-postayla giriş panelin davetine benzer bir ürün
  yoludur. **Bedeller:** süreç ölümünde tampondaki link gitmez **ve o davetin audit satırı hiç yazılmaz** (davet satırı var,
  audit yok — ADR 0022 sayılı sınır 1'in eşi); çalışan yeniden ister.

### 2.4 Bütçeler (denetim M1)

| Kova | Değer | Gerekçe |
|---|---|---|
| kaynak adres | 5 istek / 10 dk, 20 / gün | reset'in B9 kovasının (20 / 10 dk) dörtte biri |
| süreç geneli giriş alt bütçesi | 100 gönderim / saat | süreç kesicisinin (300/sa, bütün gönderimler) üçte biri — giriş, davet ve sıfırlamayı aç bırakamaz |
| işletme başına giriş daveti | 10 / saat, 30 / gün | ADR 0022 §9'un 50/300'ünün beşte biri ve onda biri |
| kişi başına giriş daveti | 1 / saat, 3 / gün | panelin 3/sa'ini tek başına tüketemez |

**Nerede sayılır:** ilk iki kova **bellekte**, süreç başına (`httpx.Limiter`; alt bütçe basımdan önce, kapıda) — pod
sayısıyla çarpılır; son iki kova **veritabanında**, `employee_invites` (`origin='self'`) satırlarından kiracı kilidinin altında.
**Mekân NAT'ı:** aynı Wi-Fi'daki personel bir kaynak adresi paylaşır; 10 dakikada altıncı kişi 429 alır — kova yükseltilmez
(adlandırılmış bedel; ekran *"use mobile data"* der). **Kesici kaderi kanalı** (bu bölüm onun yeridir; reset yolunun sayılı sınırı 13,
`adminresetoutbox.go:218-221`) burada da vardır ve daralır: eşleşmeyen adres hiçbir bütçe harcamaz, giriş gönderimleri ayrı
alt bütçeyi tüketir.

Giriş davetleri panelin ADR 0022 §9 sayaçlarında da sayılır (aynı `employee_invites` satırları): bir saldırgan bir işletmenin
50/sa bütçesinin en çok 10'unu yer — adlandırılmış bedel.

### 2.5 Tarayıcı — Q45

Öneri (b): Faz G'de **isteği** yalnız uygulama yapar. (a)'nın tarifi ve canlı *"Finish setup"* teşhis loguna bağlı başlama
kapısı Q45'te ve M16-36'dadır. **Tamamlanma her iki seçenekte tarayıcıda da olabilir:** e-postadaki link aynı telefonda
tarayıcıyı açar ve ADR 0026'nın sihirbazı onu tamamlar; owner uyarısı ve panel sinyali iki yolda aynıdır (§2.6).

### 2.6 Görünürlük: her kendi-servis aktivasyonu işverene görünür (denetim H4 (1); ikinci denetim Y2, O1)

- **Tüketim noktası `consume`'dur** (`manager.go:846`) ve `Activation.Origin`'i döndürür. Tüketen çağıranlar: tarayıcı
  `Manager.Activate` (`:717`), uygulama `ActivateDevice` (`:791`, handler `App.ActivateComplete`) ve M16-23'ün canlı QR ile müdür
  onayı yolları. Her biri tüketim döner dönmez — oturum verme adımından önce, `context.WithoutCancel` ile — tek
  `selfActivationNotice`'ı çağırır; statik bir test yeni bir tüketen çağıranın onu atlamasını yakalar.
- **Owner e-postası `Breaker.SendExempt` ile** (kesicide sayılır, asla reddedilmez — `passwordnotice.go:46`,
  `emailchangenotice.go:19` emsali), giriş çıkış kutusundan **geçmeden**, istek içinde commit'ten sonra kendi gönderim
  süresiyle (10 sn): reset formundan kesiciyi doldurmak (~960/sa tek kaynaktan, `breaker.go:16`) ya da giriş çıkış kutusunu
  doldurmak uyarıyı susturamaz. Owner başına günde 20 — bellekte `httpx.Limiter.TryCharge` (`passwordnotice.go:336-339`'un
  deseni). Owner kimlikleri `resolve_admin_owners_by_tenant` (`00081`), adresleri `passwordnotice.go`'nun alıcı yolundan.
- **Panel (dayanıklı):** M6-11 anomali ekranında *"Phones set up by email sign-in (last 30 days)"* ve **"Taps from a
  signed-out phone within 14 days of an email sign-in"**; çalışan kartında *"Set up by email sign-in on <date>"*.
- **İmzası çıkmış telefonun dokunuşu — kullanıcı onaylarsa** (M16-34'ün AskUserQuestion'ı; bugün bilerek yalnız WARN —
  ADR 0010:219-224, T59 DÜŞÜK-1): yalnız env listesindeki ve (Q49 (a)) ayarı açık işletmede, `GET /t`'nin iptal edilmiş oturum
  dalında **ilerletmeyen SUN ön kontrolü geçerse** `tap.signed_out_phone` audit satırı (`target` çalışan, `detail` yalnız kanal;
  kişi başına 10 dakikada bir). *"Plaket kanıtı taşıyan sayfa açılışı"*dır, tekrara karşı kanıt değil. Listede olmayan işletme
  (KF/KM) bayt bayt bugünkü gibi. `transactions` satırı **değildir**. Anomali yüklemi `at ∈ [used_at, used_at + 14 gün]`.
  **Uygulama yolu değişmez:** iptal edilmiş cihazın kimliği imzadan önce bir iddiadır (M16-07); **uygulamadan gelen kayıp
  dokunuşlar panelde görünmez** (adlandırılmış bedel).
- Q46 (b) aynı owner uyarı yolunu kullanır.

### 2.7 Açılış: varsayılan kapalı (ana oturum kararı) ve işletmenin seçimi (Q49)

- Env **`TAPPA_EMAIL_SIGNIN_TENANTS`** — virgülle ayrılmış tenant UUID'leri; **boş = uç her host'ta 404**; bozuk değer
  açılışı reddeder. `deploy/k8s/05-config.yaml`'a boş girer, bir paketleme testi boş diye pinler. İlk dolum ayrı bir config
  PR'ıdır — yalnız *"Taptime Demo"*; kullanıcı onaylar (birleştirme = deploy). KF/KM'nin listeye girmesi ayrı kullanıcı
  kararıdır.
- **Q49:** öneri (a) — owner'ın Account ekranındaki ayarı (`tenants.email_signin`, varsayılan **kapalı**). Uç yalnız listede
  **ve** ayarı açık işletmede basar.

### 2.8 Log ve audit

Adres, adresin özeti, kod, link hiçbir log satırında, hatada ve `detail`'de yoktur (CLAUDE.md §7; ADR 0022 §10'un kapalı
anahtar kümesi). Eşleşen işletmenin `audit_log`'unda `invite.code_emailed` (`detail.channel = "signin"`). R7A'nın tetikleyicisi
`e_?mail|address` kazanır.

## 3. Güvenlik değerlendirmesi

| Tehdit | Bugün | Bu ADR'yle | Kalan |
|---|---|---|---|
| Numaralandırma | yol yok | tek cevap + taban; eşleşmeyene e-posta yok | kaynak kovası 429'u adresten bağımsız; e-postanın gelişi yalnız kutu sahibine bilgi verir |
| **İçeriden ele geçirme** — paylaşılan kutu, yanlış yazılmış adres, deaktive edilmemiş eski çalışan (denetim H4) | müdürün e-postayla gönderdiği davet aynı kutuya düşer; saldırgan müdürün basmasını bekler | saldırgan kendisi bastırır; işverenin **herhangi bir** aktif plaketi tamamlar (girişlerde, herkese açık) — Faz D'den sonra
**bir canlı QR ekranı** da (M16-23; ekran mekânın içindedir, fotoğrafı ~20 sn yaşar) ve müdür onayı yolu (müdür onaylar); tamamlanma gerçek çalışanın oturum ve cihazlarını iptal eder (Q40 (a)) → çalışanın sonraki dokunuşu §5 satır 3'e düşer ve **kayıt yazmaz** | azaltmalar: varsayılan kapalı (§2.7), işletme kapatabilir (Q49), 7 günlük bekleme (§2.1.4), işveren davetinin korunması (§2.1.3, kilit altında), her tüketen yolda (tarayıcı, uygulama, canlı QR, müdür onayı) panel + `SendExempt` owner e-postası (§2.6; statik çağıran testi), kullanıcı onaylarsa imzası çıkmış tarayıcının plaket kanıtlı açılışı panelde (§2.6). Kalan: uygulamadan gelen kayıp dokunuşlar panelde görünmez; onay hayırsa **hiçbir** kayıp dokunuş panelde görünmez. **ADR 0005'e risk eki** (M16-34): *"posta kutusu + işverenin herhangi bir aktif plaketi = başka birinin kimliği; tespit panel ve owner e-postasıyla"* |
| Posta bombası / bütçe tüketme | — | §2.4'ün dört kovası | işletmeler arası toplam, adresi tutan işletme sayısıyla (≤ 5) sınırlı |
| Taciz (bekleyen daveti öldürmek) | — | işverenin canlı daveti varken basım yok (§2.1.3) | yalnız `self` davetler birbirinin yerini alır |
| Tenant'lar arası okuma | çözümleyiciler sırdan çözer (oturum token'ı, plaket UID'si) | çözümleyici sır **olmayan** bir girdiden çözer | emsal `resolve_admin_by_email` (`00011`); dönen yalnız kimlikler. `tappa_resolver`'ın okuduğu: `employees (id, tenant_id, email, status)` (yeni); `tenants (id, api_enabled_at, api_code)` (var olan — `00051:155`, `00105:89`) + `suspended_at[, email_signin]` (yeni; yalnız bunlar verilir ve geri alınır — `id`'ye dokunmak `/v1` kimliğini kırardı); `admin_users (tenant_id, email, …)` (var olan, `00011`). İndeks `employees (email)` (00117) |
| Askıdaki işletme | davet basabilir (§1.3) | giriş daveti basılmaz | panelin davet boşluğu ayrı kalır (bu ADR'nin kapsamı değil) |

## 4. Bedeller

1. **Kopyala-yapıştır.** `/activate` Universal Link / App Link'e bağlı değildir (Q40, M16-08); e-postadaki link aynı
   telefonda tarayıcıyı açar. Uygulamayı kullanmak isteyen çalışan linki uygulamanın yapıştırma ekranına verir.
2. **24 saatlik link** (Q44 (a)) posta kutusu ele geçirilmişse pencereyi uzatır; bugünkü 7 günlük müdür davetinden dardır.
3. **Adres bir kimlik doğrulayıcı olur.** Müdürün yazdığı adres artık aktivasyonun başlangıcıdır; yanlış adres o kutunun
   sahibine bu işletmede başkasının hesabını başlatma imkânı verir — tamamlamak plakette fiziksel dokunuş ister ve işveren
   her olayı görür (§2.6).
4. **Çıkış kutusu dayanıklı değildir** (§2.3) — süreç ölümünde link gitmez.
5. **İşverenin bütçesinden pay** (§2.4).
6. **Bekleme süresi gerçek çalışanı da bağlar:** bir ele geçirmeden sonraki 7 gün kişi kendi kendine kurtaramaz — isteği 202
   alır, e-posta gelmez; uygulamanın ekranı *"No email after a few minutes? Ask your manager for an invite."* der (müdürün
   daveti beklemeye takılmaz).
7. **Mekân NAT'ı** ve **bellekteki kovaların pod sayısıyla çarpılması** (§2.4).
8. **Owner uyarısı tamamlanmanın cevabını** en kötü durumda gönderim süresi kadar (10 sn) geciktirir.
9. **Bekleme süresi yarışı:** `consume` kiracı kilidini almaz (almak basımın kilit sırasını tersine çevirirdi); eşzamanlı bir
   `self` tüketimi ve basımı en çok bir fazla davet bırakır — kişi bütçesi (1/sa) sınırlar, o davet ayrı bir plaket dokunuşu
   ister ve tamamlanırsa ikinci bir owner uyarısı gider.
10. **Süreç alt bütçesi ve kaynak kovası bellektedir** — pod sayısıyla çarpılır.

## 5. Kırmızı çizgiler

- **§4.1, §4.2:** dokunulmaz.
- **§4.3, §4.6:** bu yol kayıt yazmaz; aktivasyonun tamamlanması da yazmaz (ADR 0026). Ele geçirmenin kayıt kaybı riski §3'te
  adlandırılmış ve azaltılmıştır.
- **§4.4:** tamamlanma `sun.Verify`'ın atomik `ctr`'ından geçer.
- **§4.5:** çözümleyici `tappa_resolver`'dır (`00011` deseni); sonrası tenant bağlamında ve açık yüklemle.
- **§4.7 + §7:** adres ve kod loglanmaz; kodun yalnız HMAC'i saklanır.

## 6. Karar verilmedi

- Q44 (TTL), Q45 (tarayıcı), Q49 (işletme ayarı) — M16-34'te cevaplanır; cevapsız kabul yok.
- KF/KM'nin env listesine girmesi (Faz G dışı kullanıcı kararı).
- Kısa kod, uygulamaya bağlı link (*Sonra*).

## 7. Uygulama

- **M16-34:** kabul; Q44, Q45, Q49; ADR 0022, ADR 0026 notları; **ADR 0005 risk eki**; canlı taban ölçümü.
- **M16-35:** sunucu — uç, çözümleyici + yetkiler (00116), indeks (00117, `00091` kalıbı), `origin`, kiracı kilidi altındaki
  kapılar, bütçeler, `EmailChannel` + çıkış kutusu sink'i, `consume`'dan owner uyarısı (`SendExempt`), panel sinyalleri,
  `tap.signed_out_phone`, env kapısı; CLAUDE.md §5 *"Aktivasyon dokunuşu"* cümlesi (kodla aynı commit). Canlıya çıkış ayrı
  config PR'ı, kullanıcı onayı.
- **M16-36:** iki uygulamanın ekranı; Q45 (a) ise tarayıcı.
