package appconfig

import "fmt"

// StripColorOrders is shared presentation order: the common GRB/RGB variants
// first. BRG is the existing production WS2811 firmware's native wire order.
func StripColorOrders() []string { return []string{"GRB", "RGB", "BRG", "BGR", "RBG", "GBR"} }

// Avoid cloning the entire catalog/configuration for every streamed frame.
func (store *Store) StripOrder() (string, string) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.value.UI.StripColorOrder, store.value.UI.StripFirmwareOrder
}

func EffectiveStripOrder(order string) string {
	if order == "" {
		return "BRG" // Preserve older configuration files and existing colors.
	}
	return order
}

func ValidateStripOrder(order string) error {
	for _, valid := range StripColorOrders() {
		if EffectiveStripOrder(order) == valid {
			return nil
		}
	}
	return fmt.Errorf("strip color order must be GRB, RGB, BRG, BGR, RBG or GBR")
}
