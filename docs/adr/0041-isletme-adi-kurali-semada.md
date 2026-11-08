# ADR 0041 — İşletme adının karakter kuralı şemada: `tenants_name_keeps_its_isolate`

- **Durum:** Önerildi (2026-11-08), denetim bekliyor.
- **Tarih:** 2026-11-08 · **Backlog:** T216 (T97'nin devamı) · **Dal:** `m14-opname` · **Migration:** 00093
- **İlgili:** CLAUDE.md §4.3 (değişmez kayıt argümanı), §4.5 (operatör `op_*` istisnası, ADR 0020/0021), §6 · 00065/00066 (NOT VALID + VALIDATE ayrımı) · 00089 (sayan ön kontrol)
- **Ölçüldü** = bu dalda, şerit veritabanı `tappa-m14-opname-db` (PG 17). **Okundu** = dosyadan, çalıştırılmadı.

## 1. Sorun

T97 ve T216 işletme adı için bir karakter kuralı koydu: bidi gömme/override/isolate (U+202A–202E, U+2066–2069), kontrol karakterleri (Cc) ve satır/paragraf ayırıcısı (U+2028, U+2029). Bunlar adın, operatör ekranının etrafına koyduğu `<bdi>`'den çıkmasını sağlayan karakterlerdir. Kural yalnız iki yazıcıdaydı: `signup.ValidateBusiness` (`CreateTenant`'tan önce) ve `tenant.Accounts.Save` (`UpdateTenantAccount`'tan önce). Okuma tarafı (`shownName`) bu kümeyi düşürüyordu. Bunun iki sonucu vardı:

- Operatör araması (`op_read_tenants`, 00063) **saklanan** metni eşler. Ekran ise **düşürülmüş** metni basar. Eski bir `Rusty<RLO>Bar` ekranda `RustyBar` görünür ama o aramayla bulunmaz (okundu).
- Şema bu kuralı bilmiyordu. Uygulamayı atlayan bir yazıcı (elle SQL, ileride gelecek bir yol) kümeden bir karakteri yine saklayabilirdi.

## 2. Karar

`tenants` tablosuna bir CHECK konur. Küme `NameBreaksIsolation`'ınkiyle aynıdır, kod noktası aralıklarıyla tek bir köşeli ifade olarak yazılır:

```
CHECK (name !~ '[\u0001-\u001F\u007F-\u009F\u2028\u2029\u202A-\u202E\u2066-\u2069]')
```

- **U+0000 kümede yok.** `text` onu hiç taşıyamaz: `chr(0)` 54000 ile reddedilir (ölçüldü).
- **Eşdeğerlik ölçülür, iddia edilmez.** `TestNameBreaksIsolation_IsTheSchemasCheck`, katalogdaki ifadeyi (`pg_get_expr`) veritabanında U+0001–U+10FFFF aralığının her kod noktası için değerlendirir (vekiller hariç; her biri `"Rusty" + r + "Bar"` içinde). Sonucu Go kuralıyla karşılaştırır: 75 kod noktası reddedilir, fark 0 (ölçüldü). Bu, `TestShownName_TheSetIsTheDomains`'in kullandığı derlemdir.
- **Arama düzeltmesi bu CHECK'in sonucudur.** Doğrulanmış bir CHECK'ten sonra hiçbir saklı ad kümeden karakter taşımaz. Saklanan metin ile ekrandaki metin bu yüzden aynıdır. `op_read_tenants` yeniden tanımlanmadı; operatörün okuyabildiği şey genişlemedi.

## 3. Göç biçimi

00093 tek bir goose transaction'ında dört adım atar: `lock_timeout = 1s`, sayan ön kontrol, `ADD … NOT VALID` ve `VALIDATE`.

- **Neden iki dosya değil (00065/00066 ayrımı).** Tek transaction'da ADD'in ACCESS EXCLUSIVE kilidi VALIDATE taramasının sonuna kadar tutulur. Bu kabul edildi, çünkü `tenants` üretimde düzinelerle satırdır (00029'un notu, okundu); taramanın kilit süresine katkısı küçüktür. Ayrık bir dosya reddedildiğinde arkasında commit edilmiş bir NOT VALID CHECK kalırdı. NOT VALID yalnız ilk taramayı atlar; o satırın sonraki her UPDATE'i yine kontrol edilir. Yani eski adlı işletmenin saat dilimi değişikliği, askıya alınması ve benzeri her yazımı 23514 ile düşerdi.
- **Üretimde ihlal varsa.** Ön kontrol yalnız sayıyı söyleyerek reddeder (kimlik yok, ad yok) ve hiçbir şey değişmez. Migrate Job düşer, rollout durur. Ad değiştirmek bir migration'ın işi değildir; bu bir veri/ürün kararıdır.
- **Eski binary yeni şemada.** main'deki `NameBreaksIsolation` bu kümeyle aynıdır (okundu, `b0a885e0`). Yani iki yazıcı CHECK'in reddedeceği her yeni ya da değişmiş adı ondan önce reddeder. Kontrolsüz geçirdikleri tek ad değişmemiş addır, Up koştuktan sonra da kayıtta öyle bir ad bulunamaz.
- **Down:** CHECK düşer.

## 4. Sınırlar

- `Accounts.Save`'in değişmemiş adı reddetmeme toleransı (`refuseHiddenName`) artık kayıtlı bir adla karşılaşmaz. Bir Down'un CHECK'siz bıraktığı şema için yerinde duruyor.
- Görünür karakteri olmayan ad (`namedVisibly`) CHECK'in konusu değil. Ekran onu yine kimliğiyle adlandırır.
- Holding adı ve signup hesap sahibinin `full_name`'i bu kuralı almadı (T97'nin bilinçli sınırı, değişmedi).
