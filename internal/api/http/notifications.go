package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	coreprofile "odin-os/internal/core/profile"
	runtimenotifications "odin-os/internal/runtime/notifications"
	"odin-os/internal/store/sqlite"
)

func handlePWAShell(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(pwaShellHTML))
}

func handlePWAManifest(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/manifest+json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(`{"name":"Odin","short_name":"Odin","start_url":"/pwa","scope":"/","display":"standalone","background_color":"#f8fafc","theme_color":"#111827","icons":[]}`))
}

func handlePWAServiceWorker(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	writer.Header().Set("Service-Worker-Allowed", "/")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(pwaServiceWorkerJS))
}

func handleNotificationsList(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	service, ok := notificationService(writer, deps)
	if !ok {
		return
	}
	records, err := service.List(request.Context(), 50)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "notifications_unavailable", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"notifications": records})
}

func handleNotificationPreferences(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	service, ok := notificationService(writer, deps)
	if !ok {
		return
	}
	view, err := service.Preferences(request.Context())
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "notification_preferences_unavailable", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, view)
}

func handleNotificationDevices(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	service, ok := notificationService(writer, deps)
	if !ok {
		return
	}
	view, err := service.Preferences(request.Context())
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "notification_devices_unavailable", err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"devices": view.Devices})
}

func handleNotificationSubscriptionCreate(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	service, ok := notificationService(writer, deps)
	if !ok {
		return
	}
	var payload notificationSubscriptionRequest
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&payload); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_notification_subscription", err.Error())
		return
	}
	device, err := service.RegisterSubscription(request.Context(), sqlite.UpsertNotificationDeviceParams{
		DeviceKey: strings.TrimSpace(payload.DeviceKey),
		Label:     strings.TrimSpace(payload.Label),
		Endpoint:  strings.TrimSpace(payload.Subscription.Endpoint),
		P256DH:    strings.TrimSpace(payload.Subscription.Keys.P256DH),
		Auth:      strings.TrimSpace(payload.Subscription.Keys.Auth),
		UserAgent: request.UserAgent(),
	}, payload.OptIn)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "notification_subscription_rejected", err.Error())
		return
	}
	writeJSON(writer, http.StatusCreated, map[string]any{"device": device})
}

func handleNotificationDeviceDelete(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	service, ok := notificationService(writer, deps)
	if !ok {
		return
	}
	view, err := service.Preferences(request.Context())
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "notification_devices_unavailable", err.Error())
		return
	}
	deviceID, err := strconv.ParseInt(request.PathValue("device_id"), 10, 64)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_notification_device_id", "device id must be an integer")
		return
	}
	for _, device := range view.Devices {
		if device.ID == deviceID {
			revoked, err := deps.Store.RevokeNotificationDevice(request.Context(), sqlite.RevokeNotificationDeviceParams{
				WorkspaceID: device.WorkspaceID,
				DeviceID:    deviceID,
				Reason:      "device unsubscribed",
			})
			if err != nil {
				writeAPIError(writer, http.StatusServiceUnavailable, "notification_device_revoke_failed", err.Error())
				return
			}
			writeJSON(writer, http.StatusOK, map[string]any{"device": revoked})
			return
		}
	}
	writeAPIError(writer, http.StatusNotFound, "notification_device_not_found", "notification device not found")
}

func handleNotificationTestEvent(writer http.ResponseWriter, request *http.Request, deps Dependencies) {
	service, ok := notificationService(writer, deps)
	if !ok {
		return
	}
	var payload struct {
		Type     string `json:"type"`
		Priority string `json:"priority"`
		Title    string `json:"title"`
		Body     string `json:"body"`
		Route    string `json:"route"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20)).Decode(&payload); err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_notification_test_event", err.Error())
		return
	}
	outcome, err := service.RouteTestEvent(request.Context(), runtimenotifications.TestEventParams{
		NotificationType: payload.Type,
		Priority:         payload.Priority,
		Title:            payload.Title,
		Body:             payload.Body,
		Route:            payload.Route,
	})
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "notification_test_event_rejected", err.Error())
		return
	}
	writeJSON(writer, http.StatusCreated, outcome)
}

func notificationService(writer http.ResponseWriter, deps Dependencies) (runtimenotifications.Service, bool) {
	if deps.Store == nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "notifications_unavailable", "notification store unavailable")
		return runtimenotifications.Service{}, false
	}
	return runtimenotifications.Service{Store: deps.Store, WorkspaceKey: coreprofile.DefaultWorkspaceKey}, true
}

type notificationSubscriptionRequest struct {
	DeviceKey    string `json:"device_key"`
	Label        string `json:"label"`
	OptIn        bool   `json:"opt_in"`
	Subscription struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256DH string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	} `json:"subscription"`
}

const pwaShellHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="manifest" href="/manifest.webmanifest">
  <title>Odin</title>
  <style>
    body { margin: 0; font-family: system-ui, sans-serif; color: #111827; background: #f8fafc; }
    main { max-width: 760px; margin: 40px auto; padding: 0 20px; }
    header { margin-bottom: 20px; }
    h1 { margin: 0 0 6px; font-size: 1.75rem; }
    button { min-height: 44px; border: 1px solid #111827; background: #111827; color: white; border-radius: 6px; padding: 0 14px; font: inherit; }
    section { border-top: 1px solid #d1d5db; padding: 18px 0; }
    ul { padding-left: 20px; }
    li { margin: 10px 0; }
    code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  </style>
</head>
<body>
  <main>
    <header>
      <h1>Odin</h1>
      <p>Action-required notifications and current operator attention.</p>
    </header>
    <section>
      <button id="enable-notifications" type="button">Enable Notifications</button>
      <p id="notification-state">Notifications are opt-in.</p>
    </section>
    <section>
      <h2>Notification Center</h2>
      <ul id="notifications"></ul>
    </section>
  </main>
  <script>
    async function refreshNotifications() {
      const response = await fetch('/notifications');
      const payload = await response.json();
      const list = document.getElementById('notifications');
      list.textContent = '';
      for (const item of payload.notifications || []) {
        const li = document.createElement('li');
        const link = document.createElement('a');
        link.href = item.route || '/pwa';
        link.textContent = item.title + ' - ' + item.status;
        li.appendChild(link);
        list.appendChild(li);
      }
    }

    async function enableNotifications() {
      const state = document.getElementById('notification-state');
      if (!('serviceWorker' in navigator) || !('Notification' in window) || !('PushManager' in window)) {
        state.textContent = 'Push unavailable on this browser. Odin will use the in-app notification center.';
        await refreshNotifications();
        return;
      }
      const registration = await navigator.serviceWorker.register('/service-worker.js');
      const permission = await Notification.requestPermission();
      if (permission !== 'granted') {
        state.textContent = 'Notifications were not granted. Odin will use the in-app notification center.';
        return;
      }
      const existing = await registration.pushManager.getSubscription();
      const subscription = existing || await registration.pushManager.subscribe({ userVisibleOnly: true });
      await fetch('/notifications/subscriptions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          device_key: 'pwa-' + Math.random().toString(36).slice(2),
          label: navigator.userAgent.includes('Mobile') ? 'Mobile PWA' : 'Desktop PWA',
          opt_in: true,
          subscription: subscription.toJSON()
        })
      });
      state.textContent = 'Notifications enabled for this device.';
      await refreshNotifications();
    }

    document.getElementById('enable-notifications').addEventListener('click', enableNotifications);
    refreshNotifications().catch(() => {});
  </script>
</body>
</html>`

const pwaServiceWorkerJS = `self.addEventListener('push', event => {
  let payload = {};
  try {
    payload = event.data ? event.data.json() : {};
  } catch (error) {
    payload = {};
  }
  const title = payload.title || 'Odin needs attention';
  const options = {
    body: payload.body || 'Open Odin for details.',
    tag: payload.tag || 'odin-action-required',
    data: { url: payload.url || '/pwa' }
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener('notificationclick', event => {
  event.notification.close();
  const url = event.notification.data && event.notification.data.url ? event.notification.data.url : '/pwa';
  event.waitUntil(clients.matchAll({ type: 'window', includeUncontrolled: true }).then(windows => {
    for (const client of windows) {
      if ('focus' in client) {
        client.navigate(url);
        return client.focus();
      }
    }
    return clients.openWindow(url);
  }));
});`
