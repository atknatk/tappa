package mt.taptime.app.device

import android.content.Context
import mt.taptime.app.activation.ActiveDevice
import mt.taptime.app.activation.DeviceState
import mt.taptime.app.activation.KeyBacking

// The activated device, in the app's private preferences. Four values and no secret: the device
// id is sent in clear on every request, the alias names a Keystore entry whose private half
// cannot leave it, the backing is a label and the name is the person's own. The invite code,
// the activation challenge and every nonce are held in memory only, for the one flow that needs
// them. Backup and device transfer are off for all app data (M16-11's manifest).
class PrefsDeviceState(context: Context) : DeviceState {
    private val prefs = context.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    override fun active(): ActiveDevice? {
        val id = prefs.getString(DEVICE_ID, null) ?: return null
        val alias = prefs.getString(KEY_ALIAS, null) ?: return null
        return ActiveDevice(
            deviceId = id,
            keyAlias = alias,
            backing = KeyBacking.of(prefs.getString(BACKING, null)),
            employeeName = prefs.getString(NAME, null).orEmpty(),
        )
    }

    override fun save(device: ActiveDevice) {
        prefs.edit()
            .putString(DEVICE_ID, device.deviceId)
            .putString(KEY_ALIAS, device.keyAlias)
            .putString(BACKING, device.backing.stored)
            .putString(NAME, device.employeeName)
            .commit()
    }

    override fun clear() {
        prefs.edit().clear().commit()
    }

    private companion object {
        const val FILE = "device"
        const val DEVICE_ID = "device_id"
        const val KEY_ALIAS = "key_alias"
        const val BACKING = "key_backing"
        const val NAME = "employee_name"
    }
}
