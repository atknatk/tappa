# Backlog — kullanıcı eylemi bekleyen işler

> **Bu dosya yalnız KULLANICININ yapabileceği işleri tutar** — fiziksel cihaz gerektiren
> ölçümler, hesap/abonelik kararları, sudo isteyen kurulumlar, dış servis kayıtları.
> Ajan bunları kodla kapatamaz.
>
> **Bu bir durum dosyası DEĞİL.** Projenin canlı durumu tek yerdedir:
> [docs/plan/state.md](plan/state.md) (CLAUDE.md §12). Buraya görev durumu, ledger
> satırı veya "sıradaki iş" yazma — çelişirler. Buradaki bir madde bir görevi
> bloklamıyorsa bunu açıkça söyler.
>
> Kullanıcı **"backlog ekle"** dediğinde madde buraya eklenir.

---

## Açık

### B1 — iOS Safari çerez ömrü ölçümü (Q11)

**Durum:** açık · **Bloklar:** hiçbir şeyi · **İlgili:** Q11, M5-01 (`a71e1b2`), M8-05

**Ne yapılacak.** Gerçek bir iPhone'da uçtan uca çerez ömrünü ölç:

1. Plakete dokun → Safari açılır → aktive ol (çerez yazılır).
2. **Günler/haftalar** bekle — arada Safari'yi normal kullan (ITP'nin tetiklenmesi için).
3. Aynı telefonla tekrar dokun → **hâlâ tanınıyor musun?**

**Neden önemli.** Ürünün "telefon seni tanır" vaadi buna dayanıyor. Çalışan uygulama
kurmuyor; tek kimlik kanıtı bu kalıcı çerez (§5'in "kim" kanıtı).

**Teknik olarak sınanan şey.** Safari ITP, **JavaScript ile yazılan** çerezleri 7 güne
kırpar. **Sunucunun `Set-Cookie` ile yazdığı `httpOnly` çerez** bu kırpmaya tabi
*olmamalı* — M5-01 tam olarak öyle yazıyor (`httpOnly`, `SameSite=Lax`,
`Max-Age=31536000`). Ölçüm bu ayrımın gerçekte tuttuğunu doğrular.

**M5-01'i bloklamadı, çünkü:** sunucu tarafı sonuçtan bağımsız — ölçüm ne çıkarsa
çıksın kod aynı. Bu yüzden bir kabul kriteri olamaz (kart düzeltmesi, 2026-07-31).
Sonuç çıkınca: [open-questions.md](plan/open-questions.md) → Q11'e yaz.

**Sonuç kötü çıkarsa** (çerez erken düşüyorsa) seçenekler: aktivasyonu daha sık
tekrarlatan bir akış, ya da çereze ek olarak ikinci bir tanıma kanıtı — **ama §4.1
gereği biyometri/WebAuthn/attestation değil.** Karar o zaman verilir.

### B3 — Saklama süresinin hukuki onayı (Q13)

**Durum:** açık · **Bloklar:** hiçbir şeyi (kod hazır) · **İlgili:** Q13, M5-02, M8-06

**Ne yapılacak.** `TAPPA_RETENTION_YEARS`'in **gerçek** değerini bir hukukçuya doğrulat
(Malta/AB istihdam + bordro/vergi saklama yükümlülüğü) ve `.env`'e yaz.

**Neden önemli.** Bu sayı **GDPR Art. 13 aydınlatma metninde çalışana gösteriliyor** —
yani hukuki bir beyan. Yanlışsa metin çalışana yanlış bilgi vermiş olur.

**Kod tarafı hazır ve bu yüzden bloklamıyor** (kullanıcı kararı 2026-07-31): sayı
**koda gömülmedi**, config'den geliyor; aydınlatma metni onu render ediyor. Dev/demo
değeri bir **varsayılan**dır, hukuki iddia değildir — kod bunu böyle söylüyor.
Doğru değer öğrenilince tek yapılacak `.env`'i güncellemek; sürüm çıkmaya gerek yok.

**Sonuç çıkınca:** [open-questions.md](plan/open-questions.md) → Q13'e yaz ve buradaki
maddeyi "Kapananlar"a taşı. Q13 ayrıca "GDPR silme talebi × immutable `transactions`"
sorusunu da taşıyor (M8-06) — saklama süresi onun bir parçası, tamamı değil.

### B2 — arm64 Go toolchain kurulumu (Q26)

**Durum:** açık · **Bloklar:** hiçbir şeyi · **İlgili:** Q26, M0-07

**Ne yapılacak.** Yerel Go toolchain'i arm64'e geçir. Tarball indirildi ve checksum'ı
go.dev ile **birebir doğrulandı** (`efb87ff2…`), ama `/usr/local`'a kurulum **sudo
parolası** istiyor — kullanıcı çalıştırmalı. Komutlar
[state.md](plan/state.md) oturum notunda.

**Kazanç yalnız hız.** Her şey bugün amd64 Go 1.26.5 ile yeşil; Rosetta altında derleme
~2-3x yavaş. Kurulunca `go version` → `darwin/arm64` olur ve ilk `make gen` bir kez
uzun sürer (build cache + pinli CLI önbellekleri tazelenir — bozukluk değil).

---

## Kapananlar

*(henüz yok)*
