//go:build darwin && cgo

package cli

import (
	"errors"
	"fmt"

	keychain "github.com/keybase/go-keychain"
)

const (
	revenueCatKeychainLabel    = "RevenueCat"
	revenueCatKeychainProtocol = "htps"
	revenueCatKeychainServer   = "app.revenuecat.com"
)

func saveRevenueCatPasswordToKeychain(email, password string) error {
	query := revenueCatKeychainItem(email, nil)
	update := keychain.NewItem()
	update.SetData([]byte(password))
	update.SetLabel(revenueCatKeychainLabel)

	if err := keychain.UpdateItem(query, update); err == nil {
		return nil
	} else if !errors.Is(err, keychain.ErrorItemNotFound) {
		return fmt.Errorf("updating RevenueCat website password: %w", err)
	}

	item := revenueCatKeychainItem(email, []byte(password))
	item.SetLabel(revenueCatKeychainLabel)
	if err := keychain.AddItem(item); err != nil {
		return fmt.Errorf("saving RevenueCat website password: %w", err)
	}
	return nil
}

func revenueCatKeychainItem(email string, password []byte) keychain.Item {
	item := keychain.NewItem()
	item.SetSecClass(keychain.SecClassInternetPassword)
	item.SetAccount(email)
	item.SetServer(revenueCatKeychainServer)
	item.SetProtocol(revenueCatKeychainProtocol)
	if password != nil {
		item.SetData(password)
	}
	return item
}
