package cli

import "testing"

func TestParseProductPriceFlags(t *testing.T) {
	prices, err := parseProductPriceFlags([]string{"usd:9.99", "EUR:8.990001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prices) != 2 {
		t.Fatalf("want 2 prices, got %d", len(prices))
	}
	if prices[0].Currency != "USD" || prices[0].AmountMicros != 9_990_000 {
		t.Fatalf("unexpected first price: %+v", prices[0])
	}
	if prices[1].Currency != "EUR" || prices[1].AmountMicros != 8_990_001 {
		t.Fatalf("unexpected second price: %+v", prices[1])
	}
}

func TestParseProductPriceFlagsRejectsDuplicateCurrency(t *testing.T) {
	_, err := parseProductPriceFlags([]string{"USD:9.99", "usd:10.99"})
	if err == nil {
		t.Fatal("want duplicate currency error")
	}
}

func TestParseProductPriceRejectsInvalidShape(t *testing.T) {
	for _, input := range []string{"USD", "US:9.99", "USD:-1", "USD:1.1234567"} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseProductPrice(input); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestFormatMicros(t *testing.T) {
	tests := map[int64]string{
		9_000_000: "9",
		9_990_000: "9.99",
		8_990_001: "8.990001",
	}
	for micros, want := range tests {
		if got := formatMicros(micros); got != want {
			t.Errorf("formatMicros(%d) = %q, want %q", micros, got, want)
		}
	}
}
