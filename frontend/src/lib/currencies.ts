// The ISO-4217 currency codes the app accepts for a travel cost — exactly the
// set the Frankfurter FX API can convert, so every stored amount is convertible
// in the admin Financial tab. Keep in sync with `supportedCurrencies` in
// backend/internal/server/submissions.go.
export const CURRENCIES = [
  'AUD', 'BGN', 'BRL', 'CAD', 'CHF', 'CNY', 'CZK', 'DKK', 'EUR', 'GBP', 'HKD',
  'HUF', 'IDR', 'ILS', 'INR', 'ISK', 'JPY', 'KRW', 'MXN', 'MYR', 'NOK', 'NZD',
  'PHP', 'PLN', 'RON', 'SEK', 'SGD', 'THB', 'TRY', 'USD', 'ZAR',
] as const

// formatMoney renders an amount with its currency using the browser's locale
// number formatting (grouping + 2 decimals), falling back to a plain fixed-2
// string if the currency isn't a recognised Intl code.
export function formatMoney(amount: number, currency: string): string {
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency,
      currencyDisplay: 'code',
    }).format(amount)
  } catch {
    return `${amount.toFixed(2)} ${currency}`
  }
}
