# ADR 0010 — Deaktivasyon tek yönlüdür, ve oturumu bilerek İPTAL ETMEZ

- **Durum:** kabul edildi
- **Tarih:** 2026-08-07
- **Karar veren:** iki ayrı kullanıcı kararı, ikisi de aynı gün.
  (1) *"Deaktivasyon `RevokeSessionsForEmployee`'yi çağırmaz"* — M6-05 A fazının
  ölçtüğü kart/kod çelişkisi üzerine (kart düzeltildi, kod kazandı).
  (2) Bu ADR, **geri alınamazlığın** kaydıdır: M6-05 B fazı deaktivasyonu sevk
  ederken ürün içinde bir *reactivate* yolu **yazmadı**, ve bunu hiçbir dosya
  söylemiyordu — ADR 0009'un tam olarak aynı sınıfı.
- **Etkilenen:** [`db/queries/employees.sql`](../../db/queries/employees.sql) ·
  [`db/queries/invites.sql`](../../db/queries/invites.sql) ·
  [`db/queries/sessions.sql`](../../db/queries/sessions.sql) ·
  [`internal/domain/tenant/staff.go`](../../internal/domain/tenant/staff.go) ·
  [`internal/handler/employeeactions.go`](../../internal/handler/employeeactions.go)
- **Migration YOK.** Şema 00003'ten beri aynı; değişen, o şemanın hangi
  geçişlerini ürünün **sunduğu**.

---

## Karar — iki parça

### 1. Deaktivasyon yalnız `employees.status` yazar; oturum iptal EDİLMEZ

Müdür "Deactivate" dediğinde tek bir alan değişir (`status = 'deactivated'` +
`deactivated_at`). Çalışanın telefonundaki oturum **canlı kalır**.

**Sonraki tap yine de `reject`'tir** — ve bunu sağlayan `sys:employee-deactivated`
guardrail'idir, oturumun ölümü değil. §5 satır 4 o tap'in **reddedilmesini**,
**kaydedilmesini** ve **güvenlik uyarısı üretmesini** ister; üçünü birden veren dal
budur.

**Neden iptal etmiyoruz** (gerekçe `db/queries/sessions.sql`'de,
`RevokeSessionsForEmployee` başlığının altında yazılı): iptal, sonucu
değiştirmez ama kişiyi **sonucun kesin olduğu** daldan, doğruluğu **her çağıranın
dikkatine bağlı** olan *"revoked session"* dalına taşır — orada bariz kısa yolu
seçen bir çağıran **hiç kayıt yazmaz**, ki bu §4.6 ihlalidir. Bedava değil, riskli.

`revoked_at` iki işini korur: **kayıp/çalıntı telefon** ve **ikinci aktivasyon**.

**Ölçüldü** (`TestEmployeeActionsDB_DeactivationMakesTheNextTapARejectThatIsRECORDED`,
gerçek Postgres + gerçek HTTP): aktif çalışan tap eder → kayıt yazılır (pozitif
kontrol) · panel deaktive eder → **canlı oturum sayısı 1'de kalır** · aynı telefon
yeniden tap eder → `verdict='reject'`, `matched_sid='sys:employee-deactivated'`,
**+1 kayıt**, **+1 `tap.security_alert`**. Mutasyon: deaktivasyona bir oturum iptali
eklendi → test **KIRMIZI** (`0 live session(s) after deactivation, want 1`).

### 2. Ürünün içinde geri dönüş YOKTUR

Deaktive edilmiş biri **ürün içinde** yeniden çalıştırılamaz. Bu, üç ayrı
kasıtlı boşluğun kesişimidir:

| Mekanizma | Ne yapar | Nerede |
|---|---|---|
| *reactivate* sorgusu **yok** | üretilen `Querier` böyle bir geçiş sunmaz | `db/queries/employees.sql` (üç `UPDATE employees`'in hiçbiri `'active'` yazmaz — o değeri yalnız aktivasyon yazar) |
| davet **reddedilir** | `ConsumeInviteAndActivate` `status IN ('invited','active')` ister | `db/queries/invites.sql` (EXISTS + dış `status IN`) |
| panelde davet butonu **kapalı** | `Person.Invitable()` false → buton yerine cümle | `internal/domain/tenant/staff.go`, `web/templates/components/roster.templ` |

İkincisi bir **kaza değil, savunma**: bir davetle yeniden aktifleştirme,
müdür-onaylı deaktivasyonun **etrafından dolaşan** bir yetki yükseltme yolu olurdu
(`invites.sql` bunu adıyla yazıyor). Üçüncüsü o kuralın ekrandaki yüzüdür.

## Sonuç — ve neden yazılması gerekti

**Yanlışlıkla "Deactivate"e basan müdürün kararı ürün içinde kalıcıdır.** Çare, o
kişiyi **yeni bir çalışan kaydı** olarak eklemektir; eski kaydı ve eski işlemleri
olduğu yerde kalır (§4.3/§4.6: hiçbir satır silinmez, kimse listeden düşmez).

⚠️ **ADR 0009 ile FARKI, çünkü ikisi aynı sanılabilir.** 0009'da geri alınamazlık
**yapısaldır** — `UNIQUE` + `REVOKE UPDATE/DELETE` + trigger, `tappa_owner` bile
yazamıyor. Burada geri alınamazlık **ürün yüzeyindedir**: şema `status`'ü geri
yazmaya izin verir (`tappa_app` `employees` üzerinde tablo-geneli `UPDATE`
tutar, 00003) ve bir migration ya da DBA müdahalesi bunu yapabilir. Yani:

| | ADR 0009 (`transaction_reviews`) | ADR 0010 (`employees.status`) |
|---|---|---|
| Ürün içinde geri alınabilir mi | hayır | hayır |
| Veritabanı seviyesinde mümkün mü | **hayır** (trigger owner'ı da reddeder) | **evet** (yetki var, sorgu yok) |
| Geri almanın audit izi | **yok** | yine **yok** (izi yazacak kod yok) |

Bu ayrım küçük görünüyor ama yön değiştiriyor: 0009'un çaresi bir **şema
değişikliği**, buranınki bir **ürün kararı** — yani *reactivate* istenirse, o
kendi kartıyla, kendi audit satırıyla ve kendi denetim turlarıyla gelir. Bugün
**alınmadı**, ve alınmama sebebi yukarıdaki ikinci satırdır: davet yolunun
kapalılığı bir güvenlik özelliğidir, ve onu açmadan bir *reactivate* yazmak
"yalnız durumu geri çevir" gibi görünen bir yetki yükseltme yüzeyi üretir.

## Ekranda ne yazıyor

Yıkıcı aksiyon **iki adımlıdır** ve ilk adım bir cümledir (script YOK, düz bir
`GET` linki — sunucunun zorlayamadığı bir tarayıcı diyaloğu onay değildir):

> Deactivating <ad> stops their taps from being accepted. Tappa has no way to undo
> it: bringing them back means adding them again as a new person, and their old
> records stay where they are.

Onaydan sonraki bant da **yapılmayanı** söyler — telefonun oturumda kaldığını —
çünkü telefonun öldüğünü sanan müdür onu izlemeyi bırakır.

## ✅ Onay adımı SUNUCUDA ZORLANIYOR (kullanıcı kararı, 2026-08-08)

Bu ADR'nin ilk hâlindeki *"iki adımlı onay"* cümlesi **ekran hakkında** doğruydu ve
**sunucu hakkında değildi**. Güvenlik denetimi ölçtü: onay GET'i **hiç istenmeden**
`POST /admin/employees/deactivate` → **303**, `status='deactivated'`. Yani bu ADR'nin
konusu olan **geri alınamaz** aksiyon, uyarı **hiç görülmeden** tek istekle
tetiklenebiliyordu — riski üreten şey iki mekanizmanın **çarpımıydı**.

**Kullanıcı kararı: zorla.** Onay ekranı, sunucunun kendi sırrından türetilmiş bir
anahtar altında **HMAC'lenmiş** bir değer üretir; değer **çalışana VE panel
oturumuna** bağlıdır ve **sunucu saatine** göre dolar
(`internal/handler/deactivateconfirm.go`; pencere TTL 10 dk **+ 1 dk** ileri-saat
toleransı = **11 dk**). Handler bu değer olmadan **hiçbir şey yazmaz**.

🔴 **TEK-ATIMLIKLIK TUTULMUYOR — ve bu ADR'nin ilk hâli tutulduğunu YAZIYORDU.**
Değer sunucuda hiçbir durum bırakmıyor; çerezi temizleyen `Set-Cookie … Max-Age=-1`
istemciye bir **rica**dır. Denetçi her POST öncesi çerezi yeniden basan bir istemciyle
**tek bir onayı üç kez** harcadı. ⚠️ Bunu daha önce yakalayamamamızın sebebi de
kaydedilmeli: replay testi `browser` yardımcısı üzerinden koşuyordu ve **o yardımcı
çerez silmeyi uyguluyordu** — yani assertion sunucunun değil, **test istemcisinin
işbirliğinin** ölçümüydü.

**Kapatılmadı, SAYILDI — gerekçesi ölçüm:** sunucu tarafı defter bir tablo/sütun
(altyapı) ister ve kazanç **sıfır** ölçüldü. Aynı aktör tek bir GET ile taze onay
üretebiliyor, ve **ikinci ve sonraki harcamalar hiçbir şey yazmıyor**:
`DeactivateEmployee`'nin kendi `status <> 'deactivated'` yüklemine çarpıp
`ErrAlreadyDeactivated` dönüyorlar — ikinci `employees` yazımı yok, ikinci
`audit_log` satırı yok, `deactivated_at` damgası **oynamıyor** (ölçüldü). Kırılan şey
veri değil, **iddiaydı**.

⚠️ **İLK SÜRÜM ZORLAMIYORDU VE BU ADR ONU "ZORLUYOR" DİYE KAYDETMİŞTİ.** O sürüm
anahtarsız bir *double-submit çerez*ti; bir denetçi kendi çerezini basıp yankılayarak
onay ekranını **hiç görmeden** birini deaktive etti (303, `status='deactivated'`).
Yorum *"logincontext.go'nun şekli"* diyordu — şekil mekanizma değildir: o dosyanın
güvencesi **anahtardan** gelir. Kalıbın **on parçası** sayılarak kopyalandı.

⚠️ **KAZANÇ KÜÇÜKTÜR ve öyle yazılmalı:** bu kapının karşısındaki aktör panelin kendi
oturumlu müdürüdür ve zaten GET-sonra-POST yapabilir. Satın alınan şey **garanti**:
yazmaya ulaşmak, sunucunun uyarıyı **o kişi için, o oturumda, on dakika içinde**
render ettiği anlamına gelir. CSRF koruması **değildir** (onu `ProtectWriting` yapar).

**İki yeni başarısızlık modu, ikisi de ekranda:** onaysız/süresi geçmiş →
*"Deactivating somebody needs the warning screen first…"* · başka kişiye bağlı onay
(ikinci sekme) → *"This browser is in the middle of confirming a different person…"*.
**Reddedilen dalda DB'de hiçbir şey kalmaz**: ne `employees` değişir, ne `audit_log`
satırı yazılır (§4.6 — ölçüldü).

Kapı, **imza**, **oturum bağlaması**, **süre** ve **ileri-saat toleransı** ayrı ayrı
mutasyonla kanıtlandı; denetçinin sekiz saldırısı + forge saldırısı bir tabloda
koşuluyor (`TestEmployeeDeactivate_TheConfirmationCannotBeForged`,
`TestEmployeeDeactivate_TheConfirmationStepIsENFORCED`,
`TestAdminConfirm_IsBoundAndExpires`), hepsi pozitif kontrollü.
⚠️ **Bu listede "tek-atımlık" YOK ve olmamalı** — ilk sürümü onu da sayıyordu, ve o
özellik tutulmuyor (yukarıdaki blok). Tutulmayanı ölçen testler:
`TestEmployeeDeactivate_TheOneShotDependsOnTheClient` ve
`TestPanelEmployeesDB_ARepeatedConfirmationWritesOnlyOnce`; ikisi de bir gün defter
eklenirse **kendilerinin silinmesini** ve bu paragrafın düzeltilmesini söylüyor.
**Ne satın almıyor:** uyarının **okunduğunu** kanıtlamıyor, ve CSRF koruması değil
(onu `ProtectWriting` zaten yapıyor).

## Kabul edilen riskler / limitler

1. **Şema hâlâ izin veriyor.** `employees.status` sütun-seviyesi bir CHECK ile
   tek yönlü yapılmadı; yapmak bir **migration** ve bir trigger ister (00003
   uygulanmış ve değiştirilemez, CLAUDE.md §6). Alınmadı, **sayıldı**.
2. **`UPDATE employees` artık üç yerde.** Eskiden bir yerdeydi ve o
   greplenebilirlik bilinçliydi; üçü de `db/queries/employees.sql`'in başlığında
   komutuyla birlikte listeli, ve **hiçbiri diğerinin yazdığı değeri yazmıyor**.
3. ✅ **Onay adımı artık zorlanıyor** (yukarıdaki bölüm) — bu satır bir limit
   olmaktan çıktı. **Kalan iki dar cümle:** (a) kapı uyarının **sunulduğunu** garanti
   eder, **okunduğunu** etmez; (b) **tek-atımlıklık istemciye bağlıdır** — çerezi
   yeniden basan bir istemci aynı onayı pencere boyunca harcayabilir; zararsızlığı
   `status <> 'deactivated'` yüklemi sağlar, kapı değil.
4. **"Sonraki tap reject" iddiası guardrail'e bağlı.** Guardrail sırası değişirse
   (ADR 0007) bu cümle yeniden ölçülmelidir; bugün ölçen test yukarıda adıyla
   yazılı ve pozitif kontrolü var.
