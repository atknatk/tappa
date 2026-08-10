# ADR 0011 — Bir düzeltme satırı vardiyayı yalnızca kısaltır, ve bıraktığı açık kayıt kapatılamaz

- **Durum:** kabul edildi (mevcut davranışın **kaydı**; kod değişmedi)
- **Tarih:** 2026-08-10
- **Karar veren:** eşleme kuralı [M6-07](../plan/m6-dashboard.md) A fazında verildi
  (`internal/domain/ledger`, `accumulate`) ve o zaman **yazma yolu yoktu**. Bu ADR
  yeni bir karar almıyor — M6-08 `transactions`'a ikinci yazıcıyı eklediğinde
  yapıcı ve **iki bağımsız denetçi** aynı sonucu ayrı ayrı ölçtü, ve hiçbir dosya
  bunu söylemiyordu.
- **Etkilenen:** [`internal/domain/ledger/report.go`](../../internal/domain/ledger/report.go)
  · [`internal/domain/manual`](../../internal/domain/manual/manual.go)
  · [`internal/handler/manualentry.go`](../../internal/handler/manualentry.go)
  · [`web/templates/pages/manualentry.templ`](../../web/templates/pages/manualentry.templ)
  · [M6-11 anomali raporu](../plan/m6-dashboard.md) (orada süresi doluyor)
- **Migration YOK.** Bu bir **belgeleme** kararıdır; eşleme motoru M6-07'den beri aynı.
- **Kardeşi:** [ADR 0009](0009-onay-karari-geri-alinamaz.md) — aynı sınıf
  (§4.3'ün *"düzeltme = yeni satır"* telafisinin **çalışmadığı** bir yüzey), ama bu
  kez **parasal**.

---

## Karar

`transactions` append-only'dir ve §4.3 telafi olarak *"yeni kayıt + `audit_log`"*
söyler. M6-08 o yolu açtı: müdür yeni bir kayıt yazabilir. **Ama raporun eşleme
motoru kişinin EN GEÇ `in`'ini ve ondan sonraki EN ERKEN `out`'unu eşliyor** — yani
her zaman kayıtların izin verdiği **en kısa** aralığı.

Sonuç iki cümlede:

1. **Eklenen bir düzeltme satırı bir vardiyayı kısaltabilir, asla uzatamaz.**
2. **Bir `in` düzeltmesinin geride bıraktığı açık kayıt hiçbir şekilde kapatılamaz.**

## Ölçüm

Gerçek `accumulate` üzerinde, bir kişi, doğru vardiya 09:00–17:00 = 8 sa
(`internal/domain/ledger/correction_test.go`, 2026-08-10):

| hata | eklenen düzeltme | önce | sonra | uygulandı mı |
|---|---|---|---|---|
| çıkış çok **ERKEN** (12:00) | `out` 17:00 | 3 sa | **3 sa** | ❌ fazlalık satır `StartedEarlier` |
| çıkış çok **GEÇ** (20:00) | `out` 17:00 | 11 sa | **8 sa** | ✅ |
| giriş çok **GEÇ** (10:00) | `in` 09:00 | 7 sa | **7 sa** | ❌ fazlalık satır `Open` |
| giriş çok **ERKEN** (08:00) | `in` 09:00 | 9 sa | **8 sa** | ✅ |

**Uygulanmayan ikisi tam olarak parayı geri getirecek olan ikisi.**

Ve `in` düzeltmesinin bıraktığı kayıt için:

| adım | worked | open | startedEarlier |
|---|---|---|---|
| yanlış çift (`in`10 / `out`17) | 7 sa | 0 | 0 |
| + düzeltici `in`09 | 7 sa | **1** | 0 |
| + kapatma denemesi `out`17:00 | 7 sa | **1** | 1 |
| + ikinci deneme `out`17:30 | 7 sa | **1** | 2 |
| + üçüncü deneme `out`18:00 | 7 sa | **1** | 3 |

Yani *"eylem gerekiyor"* listesindeki satır **kalıcı**, ve her deneme bir **kalıcı
satır daha** (§4.3 gereği silinemez) ile müdüre **bir yanlış cümle daha** yazıyor:
*"bu vardiya bu haftadan önce başladı"* — o satır hakkında iki kere yanlış (bu hafta
başladı, ve saatleri hiçbir raporda değil).

## ✅ Ürünün asıl senaryosu ETKİLENMİYOR — ve bu, kararın belkemiği

Aynı testte ölçüldü: **Q18'in kendi vakası doğru çalışıyor.**

```
kapanmamış in 09:00 tek başına   worked=0     open=1
müdür out 17:00 yazıyor          worked=8h    open=0
```

Yani M6-08'in var olma sebebi — unutulmuş çıkışı kapatmak — **kusursuz işliyor**.
Yukarıdaki tuzağa düşmek için müdürün **önce bir GİRİŞİ yanlış yazmış** olması
gerekiyor. Bu, kararı *"bozuk özellik"*ten *"sayılmış limit"*e ayıran ölçümdür.

## Neden davranış değiştirilmedi

`accumulate`'in her iki okuması da **kendi başına savunulabilir** ve kendi yorumunda
savunuluyor:

- İlk `in`'i `out` ile eşlemek **en uzun** aralığı verir → kimsenin çalışmadığı
  boşluğu bordroya yazar.
- İkisini birden eşlemek aynı süreyi **iki kez** sayar.
- İlkini açık bırakmak, kalan tek dürüst okumadır.

Aynısı `out` tarafı için: en erken `out`'u almak, ikinci bir çıkışın ilkini
"uzatmasını" engeller — ki uzatma tam olarak bordroyu **şişiren** yön.

Motoru "düzeltme farkındalığı" olacak şekilde değiştirmek, bu ADR'nin **kapsamı
dışında** bir tasarım kararıdır (aşağıya bak) ve M6-07'nin sevk edilmiş,
denetlenmiş aritmetiğine dokunur.

## Bugün ne yapılıyor

**Söyleniyor, basmadan önce.** ADR 0009'un üç seçeneğinden **üçüncüsü** —
*"hiçbir şey yapma ve ekranda söyle"* — ki M6-04 tam olarak onu yapmadığı için
eleştirilmişti. Onay ekranı, buton basılmadan **önce**:

- kaydın **kalıcı** olduğunu,
- sonraki bir kaydın vardiyayı **kısaltabileceğini ama uzatamayacağını**,
- ve **bir giriş düzeltmesinin kapatılamayan bir açık kayıt bırakacağını**

yazıyor. Bu üç cümle mutasyonla pinli (silinince testler kırmızı).

## Düzeltmek isteyen biri hangi yolu izler

Bu ADR bir yol **önermiyor** — seçenekleri ve her birinin neyi feda ettiğini yazıyor.
ADR 0009'un yapısı bilinçli olarak taklit ediliyor.

1. **Raporda supersede semantiği.** Bir kayıt sonrakini geçersiz kılabilsin (ör.
   `superseded_by` ya da *"aynı yön + aynı gün için en yeni `created_at` kazanır"*).
   **Feda edilen:** eşleşme artık `occurred_at` sırasının saf bir fonksiyonu olmaktan
   çıkar; müdür etkin sonucu istediği kadar değiştirebilir hâle gelir — 00005'in
   `transaction_reviews`'de açıkça engellediği şey. Tolere etmenin şartı her sürümün
   `audit_log`'a düşmesi ve raporun **hangi sürümü** kullandığını söylemesidir.
2. **Ayrı bir "düzeltme" tablosu.** Kayıtlar dokunulmaz kalır; üstüne append-only bir
   `transaction_corrections` gelir ve etkin durum JOIN ile okunur. **Feda edilen:**
   üçüncü bir tablo ve *"etkin kayıt"* sorusunun her okuyucuda tekrar cevaplanması.
   §4.3'ün ruhuna en yakını bu (ADR 0009'un 2. seçeneğinin aynısı).
3. **Ekranda bir "kapatılamaz" işareti.** Motor değişmez; rapor, bir açık kaydın
   **arkasında daha yeni bir `in` olduğunu** görüp o satırı *"bu düzeltmeyle
   kapandı"* diye işaretler. En ucuzu ve **yalnızca sunum**; parayı düzeltmez, ama
   müdürün sonsuza kadar kapatmaya çalıştığı satırı listeden çıkarır.

⚠️ **Bu ADR ölçtüğünü yazar, fazlasını değil.** Üç yolun hiçbiri denenmedi,
maliyetleri ölçülmedi. Kesin olan tek şey ölçülen kısımdır: eşleme en kısa aralığı
seçer, düzeltme yalnızca kısaltır, `in` düzeltmesinin bıraktığı kayıt kapanmaz, ve
Q18'in kendi vakası doğru çalışır.

## Ne zaman süresi doluyor

🔴 **[M6-11](../plan/m6-dashboard.md) (anomali ve kötüye kullanım raporu) geldiğinde.**
O görev *"eylem gerekiyor"* listesini okuyacak ve **kapatılamaz** satırları ayırt
etmek zorunda kalacak: bugün o listede birbirinden ayırt edilemeyen **üç** şey var —
gerçekten unutulmuş bir çıkış, ADR 0008 öncesi maskelenmiş bir giriş, ve **bu ADR'nin
bıraktığı kapatılamaz kayıt**. M6-11 ya üçüncüsünü işaretlemeli ya da neden
işaretlemediğini yazmalı.
