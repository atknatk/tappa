# ADR 0013 — Tenant yaratımı halka açılıyor; çözücü sırası "önce gelen" oluyor

- **Durum:** kabul edildi
- **Tarih:** 2026-08-13
- **Karar veren:** M7-02 (kayıt sihirbazı ve VAT) yapıcısı. Ö1/Ö2 kararları
  orkestratör brief'inin *"ölç, iki okumayı yaz, önerini uygula ve açıkça
  işaretle"* yetkisiyle verildi (agent-brief.md, 7. oturum kuralı).
- **Etkilenen:**
  [`db/migrations/00017_add_vat_verification_and_incumbent_first_resolution.sql`](../../db/migrations/00017_add_vat_verification_and_incumbent_first_resolution.sql)
  · [`db/queries/tenants.sql`](../../db/queries/tenants.sql)
  · [`db/queries/admins.sql`](../../db/queries/admins.sql)
  · [`internal/domain/signup/`](../../internal/domain/signup/)
  · [`internal/handler/signup.go`](../../internal/handler/signup.go)
  · [`internal/adminauth/manager.go`](../../internal/adminauth/manager.go) (davranışı
  değişmedi; **varsayımı** değişti)
- **Migration VAR:** 00017. Şema değişti (`tenants`'a iki sütun) **ve** ADR 0002
  madde 7 kapsamındaki bir **SECURITY DEFINER çözücünün gövdesi** yeniden yazıldı.

---

## Karar — iki parça

### 1. `INSERT INTO tenants` artık bir uygulama yolundan erişilebilir

M7-02 halka açık bir kayıt sihirbazı sevk ediyor. `db/queries/tenants.sql`'e
`CreateTenant`, `db/queries/admins.sql`'e `CreateAdminUser` eklendi; ikisi de
`internal/domain/signup.Provisioner`'ın **tek transaction**'ı içinde koşuyor.

**Bu, migration 00011'in adıyla yazdığı süresi dolan varsayımı bitiriyor:**

> *"NOT EXPLOITABLE TODAY, and the reason is worth naming because it expires: no
> APPLICATION path creates a tenant … **M7-02 changes exactly that.**"*

### 2. `resolve_admin_by_email` artık `ORDER BY a.created_at, a.tenant_id`

00011 `ORDER BY a.tenant_id` yazıyordu ve gerekçesi *determinizm*di. `tenant_id`
`gen_random_uuid()` olduğu için bu sıra, satırın **ne zaman yazıldığına göre
rastgele**dir. `internal/adminauth` bir istekte en fazla `MaxCandidates` (8)
adayı bcrypt ile karşılaştırdığından, **sıralama kimin parolasının deneneceğine
karar veriyor**.

---

## Neden — ölçüm

### Zarar gerçek ve ölçüldü (00017 ÖNCESİ)

Bir yıl önce kayıtlı bir "kurban" tenant'ı + aynı adresi kendi `admin_users`'ına
yazan **20 saldırgan tenant**, transaction içinde ekilip geri alındı. Beş bağımsız
koşu:

| | 1 | 2 | 3 | 4 | 5 |
|---|---|---|---|---|---|
| çözülen satır | 21 | 21 | 21 | 21 | 21 |
| kurbanın sırası | 18 | 16 | **4** | 17 | **4** |
| ilk 8'de mi? | HAYIR | HAYIR | evet | HAYIR | evet |

**Beşte üçünde ödeyen bir müşteri karşılaştırılan pencerenin dışında kaldı** —
yani doğru parolasına *"bu bilgiler çalışmadı"* cevabı aldı ve nedenini
öğrenemedi (öğrenemez de: 00011 YÜKÜMLÜLÜK 1 üç sonucun ayırt edilemez olmasını
şart koşuyor). Saldırganın bedeli 20 kayıt; müşterinin bedeli paneli.

`internal/adminauth/manager.go` bunu zaten adıyla yazmıştı: *"THE CAP CONVERTS A
CPU DoS INTO AN ACCOUNT-LOCKOUT DoS … an attacker who registers nine tenants
carrying a victim's address can push the victim's real row past the cap."*

### 00017 SONRASI — aynı sonda, beş koşu

Kurbanın sırası **5/5 koşuda 1**. 500 satırlık ölçekte de aynı: 499 saldırgan
satırı arasında kurbanın sırası **1**, ve gerçek bir `Authenticate` çağrısında
`resolved=500 compared=8 verified=1` — yani **kurbanın gerçek satırı
karşılaştırıldı ve doğrulandı**.

### Amplifikasyon — 00011'in sayısıyla karşılaştırmalı

00011 şunu yazıyordu: *"the resolver returns 500 rows in ~0.9-1.2 ms warm … ONE
unauthenticated POST /login buys ~30-50 s of CPU … ~500x amplification"*.
`internal/adminauth` bunu cost 12 için ~190 s'ye düzeltti.

Bu makinede, 00017 sonrası, aynı 500 satırlık sonda:

| ölçüm | değer |
|---|---|
| çözücü, 500 satır, sıcak | **1,22 / 1,60 / 1,22 ms** (soğuk 4,93 ms) |
| tek cost-12 karşılaştırma | 205–244 ms |
| `Authenticate`, 500 aday (3 koşu) | **1,721 / 1,701 / 1,713 s** · `compared=8` |
| kapaksız olsaydı | 500 × ~215 ms ≈ **107 s** |

**Amplifikasyon 500× değil 8×.** Kapak M6-01 B fazında sevk edilmişti; M7-02 onu
**yeniden ölçtü, açmadı**, ve `adminAttemptLimit` (10 başarısız/10 dk/adres) ile
birlikte pencere başına ~17 s CPU/adres ≈ **bir çekirdeğin %2,9'u**.

### 00011'in üç seçeneği — hangisi seçildi

| seçenek | karar |
|---|---|
| (a) tek istekte doğrulanacak aday sayısını sınırla | **ZATEN SEVK EDİLMİŞ** (`MaxCandidates = 8`, M6-01 B). M7-02 yeniden ölçtü. |
| (b) ilk eşleşmede dur | **REDDEDİLDİ.** 00011 bunun YÜKÜMLÜLÜK 5 ile çarpıştığını söylüyor; `adminauth` ayrıca **ürünü bozduğunu** ölçmüş: aynı kişinin iki işletmesi genelde aynı parolayı taşır, "ilk eşleşme" seçiciyi görünmez kılar. |
| (c) e-posta doğrulaması | **YAPILAMADI, ve iddia edilmiyor.** E-posta taşıyıcısı bu repoda **yok** (Q02 açık, M7-04'ün işi). Kapatıcı çözüm budur ve devredildi. |
| (d) **sıralamayı ilk-gelene çevir** | **SEÇİLDİ.** 00011'in listesinde yoktu; (b)'nin YÜKÜMLÜLÜK 5 çarpışması yok (küme daralmıyor, sırası değişiyor), (a)'nın kilitlenme bedelini **mevcut müşteri için** ortadan kaldırıyor, (c)'nin altyapısını gerektirmiyor. |

---

## 🔴 Risk 1 — DÜZELTMENİN KENDİSİ İKİ KANAL AÇTI (ve ikisi de KAPATILDI)

> ⚠️ **Bu bölüm iki tur boyunca *"kapatılamaz"* diyordu ve iki kez yanılmıştı.**
> Aşağıda önce açılan kanal, sonra yanlış çıkan iki iddia, sonra kapatan mekanizmalar.

**Bu ADR'nin en önemli bölümü: 2. turda bir denetim kanalı buldu, 4. turda bir başkası
bu bölümün "kapatılamaz" gerekçesini çürüttü.**

Sıralama, yerleşiğin pencerenin **ilk** sırasını güvenilir biçimde işgal etmesini —
dolayısıyla **başkasının parolasına karşı güvenilir biçimde başarısız olmasını** —
sağlıyor. Bir adres için tam `MaxCandidates` satır ekip **kendi** parolasıyla giriş
yapan saldırgan, seçicinin satır sayısını *"bu adresin Tappa hesabı var mı?"*
sorusunun cevabı olarak okuyor. `adminauth.Authenticate` üzerinden ölçüldü, 3/3:

| | resolved | compared | truncated | **seçici gösteriyor** |
|---|---|---|---|---|
| adres bilinmiyor | 8 | 8 | false | **8** |
| adres **KAYITLI** | 9 | 8 | true | **7** |

Bu, 00011'in **YÜKÜMLÜLÜK 1**'inin cevaplamamak için bir dummy bcrypt harcadığı soru.

⚠️ **Sıralama kanalı YARATMADI, DETERMİNİSTİK yaptı.** `ORDER BY tenant_id` altında
yerleşik pencereye 8/9 olasılıkla giriyordu, yani aynı sayı **gürültüyle** sızıyordu.
Bu migration olasılıksal bir sızıntıyı **kesin** hâle getiriyor.

### 🔴 "Doyma altında sinyal yok" YANLIŞTI — ve bu ADR'nin en kötü cümlesiydi

**Seçici sayısı** doyma altında değişmiyor:

| ekilen | bilinmiyor | KAYITLI |
|---|---|---|
| 3 | gösterir 3 | gösterir 3 |
| 5 | gösterir 5 | gösterir 5 |
| 8 | gösterir 8 | **gösterir 7** |

Ama bu **yalnız sayı kanalı** için doğru. **Yanıt SÜRESİ** doyma noktasının çok
altında değişiyor, çünkü `Authenticate` penceredeki her adayı karşılaştırıyordu ve
**dolgu yoktu**. Sevk edilen kodda, **TEK** ekili satır, 9 çapraz turlu:

| kol | medyan | aralık |
|---|---|---|
| adres **BİLİNMİYOR** | **216 ms** | 208–226 |
| adres **KAYITLI** | **437 ms** | 422–478 |

**Delta 220 ms (%102), iki aralık HİÇ ÖRTÜŞMÜYOR.** Yani YÜKÜMLÜLÜK 1'in reddettiği
soru **tek bir kayıtla** cevaplanıyordu — sekizle değil — ve bu ADR'nin ilan ettiği
*"adres başına 8 kayıt, iki saat"* bedeli **yanlış kanala** fiyatlandığı için **8×
fazlaydı**.

### ✅ İki kanal da kapatıldı — ve ikisi de bu sıralama DEĞİL

| kanal | kapatan | bedel |
|---|---|---|
| **yanıt süresi** | `adminauth.padComparisons` — her giriş **tam `MaxCandidates`** karşılaştırma yapar, eksiği dummy ile | giriş başına **~216 ms → ~1,75 sn** |
| **seçici sayısı** | `adminauth.PickerCap = MaxCandidates − 1` — **gösterilen** liste kapağı | 8 işletmede doğrulanan operatör **bir girdi** kaybeder |

🔴 **"Dolgunun DoS bedeli yok" DENMİŞTİ VE BU YANLIŞTI — ve bu ADR'nin en pahalı
hatasıydı, çünkü kanalı kapatma kararı o cümleye dayanılarak verildi.**
`adminAttemptLimit` **yalnız başarısızlıkları** sayıyor (`failLogin` charge ediyor,
`completeLogin` **hiç** etmiyor). Güvenlik denetçisinin sevk edilen yığında ölçümü:

```
18 ardışık BAŞARILI giriş → 18/18 303, 429 sayısı: 0
13 ardışık BAŞARISIZ giriş → ilk 10'u 401, #11–13 → 429
```

Yani dolgunun **başarı yolundaki** bedeli yalnızca `adminFloodLimit` (3000/10 dk) ile
sınırlıydı: 3000 × 8 × 0,38 sn ≈ **9 120 CPU-sn / 600 sn ≈ 15 çekirdek**, tek adresten.

✅ **Kapatan:** `adminLoginWorkLimit` — parola döngüsüne **ulaşan her girişi**, sonucu
ne olursa olsun, **120 / 10 dk / adres** ile sınırlıyor. Rakam bir CPU hedefinden değil,
`adminratelimit.go`'nun **kendi ofis modelinden** türetildi (10 admin × 2 cihaz × 2 pay).
Yeni tavan: 120 × 8 × 0,38 ≈ **365 CPU-sn/pencere/adres ≈ 0,61 çekirdek**.

🔴 **VE BU YOL M7-02'DEN ÖNCE DE BÜTÇESİZDİ** — bu, yalnız kendi açtığımız gediği
kapatmak değil. Dolgudan önce giriş başına tek karşılaştırmayla aynı 3000'lik tavan
**~1,9 çekirdeğe** izin veriyordu ve onu da hiçbir şey ölçmüyordu; dolgu bunu 8×
büyüterek **görünür** yaptı. **0,61 çekirdek, M7-02 öncesi rakamın da altında** — yani
burada sevk edilen şey bir onarım değil, **bu milestone'dan eski bir deliğin
kapatılması**.

**§4.6'ya değmesinin sebebi:** tap yüzeyi aynı süreci ve aynı havuzu paylaşıyor; panel
selinin aldığı CPU, tap'in alamadığı CPU'dur, ve servis edilemeyen bir tap
**yazılmayan bir mesai kaydıdır**.

**İki okuma, açıkça:**
- **AÇIK BIRAK:** sıradan giriş ~216 ms kalır, ve bir kayıt *"bu adres kayıtlı mı?"*
  sorusunun güvenilir cevabını satın alır — ürünün her başarısız girişte bir dummy
  bcrypt harcayarak reddettiği soru.
- **KAPAT:** her giriş ~1,75 sn sürer, **başarılı olanlar dahil**.

**KAPATMA seçildi**, çünkü DoS tavanı zaten sekiz karşılaştırma için yazılmıştı ve
gecikme günde bir-iki kez yapılan bir yönetim işlemine düşüyor, tap yoluna değil.

### Dört kapatma denendi ve ÖLÇÜLDÜ — dördü de daha kötü (ve BEŞİNCİSİ kaçmıştı)

| # | deneme | ölçüm | sonuç |
|---|---|---|---|
| 1 | **Çözülen her adayı karşılaştır** (kapağı döngüden sonra uygula) | 200 aday = **45,2 sn CPU tek istekte**; 500'e ölçeklenince **~1 dk 53 sn** | Kanalı **tam kapatıyor**, ama kapağın var olma sebebi olan DoS'u geri getiriyor |
| 2 | **Bir e-postanın açabileceği işletme sayısını sınırla** (ör. 3) | Aynı kehanet yeni sınırda: **3'e karşı 2** | Fiyatın **üçte birine** aynı sinyal → **kesinlikle daha kötü** |
| 3 | **En eskiyi pencereye EK olarak al** (K+1) | ekilen 8: **8'e karşı 8** ✅ · ekilen 9: **9'a karşı 8** ❌ | Doyma noktasını **bir kaydırıyor**, +1 bcrypt |
| 4 | **Önek yerine rastgele örnek** (sayıyı gürültülendir) | 900 denemede yerleşik **%10,1**'inde hiç karşılaştırılmıyor | **Aralıklı kilitlenme** — bu migration'ın düzelttiği kusurun teşhisi daha zor hâli |

🔴 **"GENEL İFADE" GERİ ÇEKİLDİ.** Burada şu yazıyordu: *"çağıranın doyurabildiği her
sınır, sinyali sınırın kendisinde yeniden üretir"*. Bir denetim bunu **çürüttü** —
**beşinci** kapatma, karşılaştırılan **pencereyi** değil **gösterilen listeyi**
sınırlıyor. Görüntüleme kapağı karşılaştırma kapağının **bir altına** konunca,
yerleşiğin işgal ettiği yuvayı **tam olarak** soğuruyor: bilinmeyen `min(k,C,P)`,
kayıtlı `min(k,C−1,P)`, ve `P = C−1` ile ikisi **her k için eşit**. k = 0…40 ölçümü:

```
pickerCap=8 → sızdıran k: [8 9 10 … 40]
pickerCap=7 → sızdıran k: []          (hiçbiri)
```

**Dört kapatmanın aynı şekilde başarısız olması, her kapatmanın başarısız olacağının
kanıtı değildi** — dördünün de **aynı mekanizmaya** nişan aldığının kanıtıydı.
YÜKÜMLÜLÜK 5 ihlal edilmiyor: küme **daralıyor** (daraltma yalnız erişimi
reddedebilir, veremez).

### Ne AÇIK kaldı

`internal/adminauth`'ın zaten belgelediği ve bütçelediği **`audit_log` yazma
asimetrisi** (yanıtın ~%1–3'ü). Dolgudan sonra oransal olarak küçüldü, yok olmadı.

### Sonda yine de GÖRÜNÜR kılındı

Sonda **görünür** ve **sınırlı** kılındı: `AdminAuth.recordCandidateProbe` bir
`audit_log` satırı yazıyor — **digest'i gerçekten doğrulanmış** bir tenant'a, yani
saldırganın kendisininkine, ki sondayı bir tenant id'sine ve oradan `signup.completed`
üzerinden bir **VAT numarasına** bağlayan şey budur — ve mevcut hesap bütçesiyle
ölçülüyor, yoksa hiç kimsenin silemeyeceği bir tabloya sınırsız yazma olurdu.
`admin.login.candidates_truncated`; WARN satırı artık girişin **başarılı olup
olmadığını** da söylüyor.

⚠️ **Bu bir savunma değil.** Sondayı yavaşlatmıyor, reddetmiyor, daraltmıyor.
**Görülebilir kılıyor.**

🔴 **VE İMZASI SANILDIĞI ŞEY DEĞİL.** İlk hâli *"neredeyse hiç meşru karşılığı yok —
gerçek bir operatörün dokuz işletmesi olması gerekirdi"* diyordu. Ölçüldü: kurbanın
adresine sekiz satır ekiliyken **kurbanın KENDİ doğru girişi** de `truncated=true`
üretiyor ve satır **kurbanın tenant'ına** yazılıyor. Yani imza *"biri sonda yapıyor"*
değil, ***"bu adres aşırı-kayıtlı"***; saldırgan böylece **dolaylı olarak** üçüncü bir
tarafın silinemez `audit_log`'una kalıcı satır yazdırabiliyor. Çağıran satırı
**yönlendiremiyor** (tenant doğrulanmış digest'ten gelir, istekten değil) ve hesap
bütçesi sayıyı bağlıyor — ama bu iddia edilmeden bırakılmıyor.

---

## ✅ Kabul edilen risk 2 — ARTIK KAPATILAN yarı: yeni müşteri hiçbir şey öğrenmiyordu

Bu ADR sıralama takasını şununla savunuyordu:

> *"kaydını tamamlayamayan müşteri bunu **bizim sayfamızdayken** öğrenir"*

**Takasın o yarısı üründe YOKTU** ve bir denetim uçtan uca ölçtü: sekiz satır ekili bir
adrese gerçek bir kayıt tamamlandı → **303**, **APPROVED** damgalı onay ekranı,
*"Sign in — use the email address and password you just chose."* → tam o kimlik
bilgileriyle `POST /admin/login` → **401**, ve giriş ekranı **açıklama yapamaz**
(YÜKÜMLÜLÜK 1). Müşteri, davranabileceği tek anda **hiçbir şey** öğrenmedi.

**Eksik yarı inşa edildi.** `Provisioner.signInBlocked` transaction **commit olduktan
sonra** adresi çözüyor ve az önce yazdığı satırın giriş penceresinin içinde olup
olmadığını ölçüyor; onay ekranı da çalışmayacak bir girişe davet etmek yerine bunu
söylüyor. Yalnız **bir kayıt tamamlanarak** ulaşılabiliyor ve **tek bir bit** üzerinde
dallanıyor.

### 🔴 Ama o bit de bir kanal — ve bu ADR bunu beş yerde inkâr ediyordu

*"Bir, iki ya da yedi hesabı olan bir adres bilinmeyen bir adresle aynı cevabı
verir"* cümlesi **yalnız saldırgan hiç satır ekmezse** doğru — ve satır ekmek bu
sayfadaki her kontrolün **varsayımı**. Gerçek Postgres'te ölçüldü: bir adrese
`MaxCandidates − 1` satır ek, sonra **bir kayıt daha** tamamla.

| adres | toplam kimlik | `SignInBlocked` |
|---|---|---|
| bilinmiyor | 8 | **false** |
| bir mevcut hesap var | 9 | **true** |

⚠️ **Ve ekleme sayısı değiştirilirse tek bit değil:** farklı sayılarda kayıt
tamamlayıp bayrağın nerede döndüğüne bakan biri, önceden var olan kimlik sayısını
(`k`) **tam olarak** öğrenir.

Bu, 00017'nin **2. kapatma** olarak reddettiği şeklin ta kendisi (*"aynı kehanet
yeni sınırda yeniden belirir"*) ve **geri çekilen genel ifadenin** tarif ettiği şey.
Tek fark önemli: seçici sayısı bir **görüntü** capıydı ve daha dar bir görüntü capı
(`PickerCap`) onu soğurabildi; bu ise **karşılaştırma penceresinin kenarının
raporu**, ve müşteriye hesabının çalışmayacağını hâlâ söyleyen daha dar bir rapor
yok.

**Kapatılmadı — SAYILDI ve GÖZLENEBİLİR kılındı:**

| | |
|---|---|
| **fiyat** | sondalanan adres başına `MaxCandidates` **tamamlanmış kayıt**, her biri ayrı **küresel tekil VAT** + bir `signupCreateLimit` birimi (3/saat/adres) ⇒ tek adresten **~3 saat**; kapatılan zamanlama kanalından **~8× pahalı** |
| **iz** | `signup.sign_in_unreachable` — kaydın **yeni yarattığı** tenant'a, zaten koştuğu create bütçesiyle sınırlı, sayıları taşıyor, **adresi taşımıyor** (§4.7) |
| **alternatif** | Kaldırmak, bir denetimin uçtan uca ölçtüğü kusuru geri getirir: gerçek müşteri **APPROVED** damgası alır, sonra açıklama yapamayan bir ekranda **401**. Ödeyen bir müşterinin **kesin** kaybı, pahalı + sayılmış + gözlenebilir bir sinyalden kötüdür. |

## Kabul edilen risk 3 — kapatılmayan yarı

🔴 **ÖN-KAYIT VARYANTI AÇIK.** Bir saldırgan, bir adres **hiç kayıt olmadan
önce** o adresle 8+ satır ekerse, pencereyi tümüyle sahiplenir ve gerçek müşteri
— sonra gelen, sonra sıralanan — **kayıt olduğu günden itibaren giriş
yapamaz**. Sıralama saldırıyı *hacim* gerektirmekten çıkarıp *öngörü*
gerektirmeye çevirir; bu büyük ve pratik bir fark, **kanıt değil**.

⚠️ **VE YENİ MÜŞTERİ İÇİN BU BİR GERİLEME.** `ORDER BY tenant_id` altında yeni
bir kayıt rastgele bir yere düşer ve bazen karşılaştırılır; `created_at` altında
**deterministik olarak sona** düşer. Takas bilinçli: kaydını tamamlayamayan
müşteri bunu **bizim sayfamızdayken** öğrenir; bir yıldır kullandığı panelden
sessizce kilitlenen müşteri bunu **bordro günü** öğrenir.

**Kapatan şey:** e-posta doğrulaması (00011'in üçüncü seçeneği) — M7-04.

### Bedeli sınırlayan ikinci mekanizma

`internal/handler/signupratelimit.go` **`signupCreateLimit = 3 / saat / adres`**
getiriyor. Pencereyi dolduracak 8 satır için bir adresten **üç saat** gerekiyor.
Bu bir kapatma değil, **bedel**; ve `httpx.Limiter`'ın kendi sınırı geçerli
(süreç içi, sabit pencere, dağıtık kaynağa karşı orantılı).

---

## §4 kırmızı çizgilerine etkisi

- **§4.5 (tenant izolasyonu):** Değişmedi ve ölçüldü. Yeniden yazılan fonksiyon
  ADR 0002 madde 7'nin sekiz koşulunu **birebir** koruyor (`owner=tappa_resolver`,
  `prosecdef=true`, `search_path=pg_catalog, pg_temp`, şema-nitelikli gövde,
  `OPERATOR(public.=)`, sütun-düzeyi SELECT, `public` EXECUTE **yok**,
  `tappa_app` EXECUTE **var** — hepsi `pg_proc`'tan doğrulandı). `created_at`
  granta eklendi; **döndürülmüyor**, yalnız sıralamada kullanılıyor.
  Sihirbazın yeni tenant'ı, mevcut tenant'lardan **filtresiz** sondalarla izole
  ölçüldü (pozitif kontrolüyle birlikte).
- **§4.6 (kayıt kaybolmaz):** VIES cevabı `vat_verified` **NULLABLE** olarak
  saklanıyor. `boolean NOT NULL` bir kesintiyi *"bu VAT numarası geçersiz"* diye
  yazmak zorunda kalırdı — bir arızadan üretilmiş suçlama. Dört durum:
  hiç sorulmadı · soruldu cevap yok · geçerli · geçersiz.
- **§4.7:** Sihirbaz parolayı hiçbir yere yazmıyor: imzalı çerezde **yok**
  (hesap adımı bu yüzden **son**), gövdede **yok** (reddedilen form parolayı geri
  basmıyor), log'da **yok**, `Result`'ta **alan yok**.

## Kapatılmayan, sayılan diğer sınır

`internal/adminauth/password.go` **dördüncü bir zamanlama kolu** adlandırıyor:
geçerli bcrypt olmayan bir `password_hash` ~1,9 milyon kat hızlı cevap veriyor,
ve yapısal çözümü bir `CHECK` kısıtı. **00017 bunu EKLEMEDİ.**

🔴 **BU ADR'NİN İLK HÂLİ GEREKÇEYİ YANLIŞ ÇERÇEVELEDİ** ve 2. turda bir denetim
düzeltti. *"25 266 satırın 15 340'ı geçmezdi"* cümlesi bir **veri gerçeği** gibi
okunuyordu; değil. İki engel **cinsleri farklı**:

- **ÜRÜN VERİSİ TEMİZ.** İki seed tenant'ına ait `admin_users` satırlarının
  **tamamı** deseni geçiyor (tenant **id**'siyle ölçüldü — *isimle* değil:
  `Kebab Factory Ltd` bu reponun kendi testlerinin on binlerce tenant'a taktığı bir
  isim ve `adminratelimit.go` tam bu tuzağı kaydediyor). **Temiz kurulmuş bir
  veritabanına düz `CHECK` bugün eklenebilirdi.**
- **GELİŞTİRME VERİTABANI KİRLİ**, ve bu **test kalıntısı**: `admin_users`
  satırlarının çoğunluğu binlerce tenant'a yayılmış `x` / `not-a-real-hash`
  placeholder'ları taşıyor. Buraya **sayı yazılmıyor** — bu tablo her `make test`
  koşusunda büyüyor. Bu engel **T26/T22** (test kirliliği + temizlemeyen `db-reset`),
  bu migration'ın değil.
- **KALAN GERÇEK ENGEL:** `CHECK … NOT VALID` mevcut satırları taramaz ama **yeni**
  satırlarda zorlar, ve o placeholder'ları **beş test dosyası** yazıyor
  (`internal/db/admins_test.go`, `internal/db/rls_test.go`,
  `internal/domain/tenant/rulebook_db_test.go`, `.../rulewriter_db_test.go`,
  `internal/domain/billing/billing_db_test.go`).

M7-02'nin açtığı yazma yolunda kural **disiplinle + testle** tutuluyor: sihirbaz
`password_hash`'e yalnız `adminauth.Hash` çıktısını yazıyor ve yazılan satırın
`$2a$12$` ile başladığı gerçek Postgres'e karşı ölçülüyor. Yapısal kapatma, o
fixture'ları düzeltecek göreve devredildi.

## Ve bir üçüncü, ürün tarafında: VAT sonucu **panelde görünmüyor**

`vat_verified` yazılıyor ama **hiçbir sorgu okumuyor** ve panelde tek bir VAT satırı
yok; 00017 UPDATE grant'ini de bilerek vermiyor. Done ekranının ilk hâli *"dashboard'da
göreceksiniz"* diyordu — M7-01'in bloklayan bulgusunun **bir görev sonra** tekrarı,
üstelik APPROVED damgasının altında. **Cümleler ürünün bugün yaptığına indirildi**
(damga doğruydu, not yanlıştı) ve kısıt **türetilmiş** bir testle tutuluyor:
`TestSignupDone_PromisesNoPanelSurfaceForTheVATCheck` üretilen sqlc satır tiplerini
tarıyor, bir panel ekranı sütunu **okumaya başladığı gün** cümle serbest kalıyor.
Paneli göstermenin iki adayı **ölçülüp reddedildi**: paylaşılan chrome **her panel
isteğine bir okuma** ekliyor, billing ekranı ise M6-12'nin çitli yüzeyine altı dosya
+ M7-05'in sonra taşıması demek.
