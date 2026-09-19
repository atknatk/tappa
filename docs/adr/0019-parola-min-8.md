# ADR 0019 — Panel parolasının alt sınırı on iki değil sekiz rune

- **Durum:** kabul edildi
- **Tarih:** 2026-09-19
- **Bağlam:** `internal/domain/signup` `MinPasswordRunes` sabiti — kayıt sihirbazı
  (ADR 0013), parola digesti (ADR 0014) ve sıfırlama akışı (ADR 0015) hepsi bu
  tek sabiti okur. Kullanıcı ürün kararı: **"basit olsun."**
- **İlgili:** [ADR 0014](0014-parola-digestinin-tabani-islenebilirlik.md)
  (bcrypt cost 12 + 72-bayt tavan — **dokunulmadı**) ·
  [ADR 0015](0015-sifirlama-tokeni-tek-gecislik-yetkidir.md) (sıfırlama akışı aynı
  sabiti okur) · [ADR 0013](0013-kayit-sihirbazi-ve-ilk-gelen-sirasi.md) (ilk
  operatör kaydı) · CLAUDE.md §4 (kırmızı çizgiler), §7, §8, §9 · dosyalar:
  `internal/domain/signup/signup.go`, `internal/handler/adminreset.go`,
  `web/templates/pages/signup.templ`, `web/templates/pages/account.templ`

## Neden bir ADR

Bu bir **güvenlik sınırı değişikliğidir** (CLAUDE.md §10: "güvenlik sınırı
değiştiyse ADR yaz"). Bir parola politikasının **zayıflatılması** — alt sınırın
düşürülmesi — sessizce yapılamaz; bilinçli, kullanıcının ürün kararıdır ve elenen
alternatif (sınırı yerinde bırakmak) ile birlikte, ödenen bedelin fiyatıyla
kayda geçer.

## Bağlam

Bugün `MinPasswordRunes = 12`: panel operatörü bir parola seçerken (kayıt,
`internal/handler/account` üzerinden değiştirme ve `internal/handler/adminreset`
sıfırlama akışlarının üçünde de) en az **on iki rune** girmek zorunda. Sabit
**tek kaynaktır**: doğrulama (`signup.ValidateAccount`), sıfırlama
(`adminreset.checkNewPassword`) ve kullanıcıya görünen metinler
(`AdminResetNewView.MinPasswordChars`, signup formu) hepsi bu tek sayıyı okur;
yalnız iki HTML ipucu ve bir `minlength` özniteliği sabiti değere gömüyordu (bu
turda onlar da tek kaynağa göre 8'e çekildi).

On iki rune, ürünün kendi metninde (`signup.go` sabit yorumu) **kompozisyon
kuralı değil, bir taban** olarak savunuluyor: "bir büyük harf, bir rakam, bir
sembol" gibi kurallar insanları ölçülebilir biçimde `Password1!`'e itiyor;
güvenlik yükünü **bcrypt cost 12** (offline tahmini pahalı yapar) ve **panelin
deneme bütçesi** (online tahmini anlamsız yapar) taşıyor.

## Karar

`MinPasswordRunes` **12 → 8**. Başka **hiçbir şey** değişmez:

1. **Bcrypt cost 12** (ADR 0014, `internal/adminauth`) — aynı. Offline tahmin
   maliyeti değişmedi.
2. **`MaxPasswordBytes = 72`** (bcrypt'in bayt tavanı, ADR 0014) — aynı. Üst sınır
   ve onun "bytes, not runes" gerekçesi yerinde.
3. **Kompozisyon kuralı yok** — hâlâ yok. Değişen tek şey **tabanın yüksekliği**.
4. **Deneme bütçesi / rate limit** (`adminLoginWorkLimit`, `adminResetUnknownLimit`)
   ve **hesap-başına izolasyon** — aynı.

Değişiklik tek sabitte yapıldığı için üç akış (kayıt, değiştirme, sıfırlama) ve
tüm kullanıcıya görünen metinler birlikte, tutarlı biçimde 8'e iner — "kapının
yarısı 8 yarısı 12" tutarsızlığı yapısal olarak imkânsızdır.

## Gerekçe — ve ödenen bedel

- **Neden 8:** Kullanıcının açık ürün kararı — **"basit olsun."** On iki rune,
  kayıt sırasında (ürünün en kırılgan hunisi) bir sürtünmedir; sekiz, yaygın
  parola-yöneticisi ve kurumsal politika tabanıyla uyumlu, akılda kalır bir
  eşiktir.
- **Bedel — ölçülü ve açık:** Sekiz karakterlik bir parola, on ikiden **ölçülebilir
  biçimde daha zayıftır** (brute-force uzayı üstel olarak küçülür). Bu bedel
  bilerek kabul edilir, çünkü tabanın tek başına taşıdığı yük değil, **savunma
  derinliği** önemlidir: offline sızıntıda **bcrypt cost 12** her tahmini pahalı
  tutar; online denemede **rate limit** eşiği tüketir; bir hesabın düşmesi
  **hesap-başına** kalır (paylaşılan sır yok). Alt sınır bu üç lever'ın *önündeki*
  ilk süzgeçtir, tek savunma değil.
- **Elenen alternatif — sınırı 12'de bırakmak:** Reddedildi. Güvenlik marjı
  marjinal olarak daha yüksek olurdu, ama kullanıcının açık kararına ve ürünün
  "sıfır sürtünme" ilkesine (CLAUDE.md §9) aykırı; ve gerçek güvenlik yükünü
  taşıyanlar (bcrypt, rate limit) zaten yerinde.

## Sonuçlar

- **Tek sabit:** `signup.MinPasswordRunes = 8`. Kod yorumu on iki→sekiz gerekçesini
  ve dokunulmayan lever'ları anlatır ve bu ADR'ye atıf verir.
- **Kullanıcıya görünen metinler:** signup formu (`minlength="8"` + ipucu),
  hesap parola formu ipucu, sıfırlama formu (`MinPasswordChars` üzerinden dinamik)
  hepsi "at least 8 characters" der.
- **Testler:** `internal/domain/signup` sınır-değeri testi taban sabitine göre
  simgesel (`MinPasswordRunes-1` = **7 red**, `MinPasswordRunes` = **8 kabul**),
  yani sabit değiştiğinde sınır kendiliğinden 7/8'e taşınır. Handler tarafındaki
  metin iddiaları "at least 8 characters"'e güncellendi; "too short" senaryoları
  (6/5/3 rune) yeni tabanın altında kalmaya devam eder.
- **Dokunulmayan güvenlik:** bcrypt cost 12, `MaxPasswordBytes = 72`, kompozisyon
  kuralının yokluğu, rate limit — hiçbiri değişmedi (§4 kırmızı çizgilerin hiçbiri
  bu değişiklikle ihlal edilmez; bu bir güven-derinliği ayarıdır, bir sınırın
  kaldırılması değil).
