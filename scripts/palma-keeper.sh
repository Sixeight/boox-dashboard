#!/usr/bin/env bash
# Restores the Palma's dashboard whenever it (re)connects over USB.
# A missing adb reverse mapping is the signal: it is lost on unplug, device reboot, or adb server restart.
set -u

PORT=${PORT:-8080}
URL="http://localhost:${PORT}/"
CHROME=org.chromium.chrome
# Upside down, so the cable can stay plugged in while the Palma stands upright. 0/1/2/3 = 0/90/180/270 degrees.
ROTATION=${ROTATION:-2}
# The screen center, which lands on the page in either orientation; a tap is the user gesture that requestFullscreen needs.
TAP_X=412
TAP_Y=824

has_reverse() {
	case "$(adb reverse --list 2>/dev/null)" in
	*"tcp:${PORT} tcp:${PORT}"*) return 0 ;;
	*) return 1 ;;
	esac
}

# Each step must succeed: the reverse mapping marks setup as done, so a failure drops it to retry on the next poll.
setup() {
	echo "palma connected; restoring dashboard"
	adb reverse "tcp:${PORT}" "tcp:${PORT}" &&
		# The Palma has no network, so its clock drifts; take the host's.
		adb shell cmd alarm set-time "$(date +%s%3N)" &&
		adb shell settings put global stay_on_while_plugged_in 3 &&
		adb shell settings put system accelerometer_rotation 0 &&
		adb shell settings put system user_rotation "$ROTATION" &&
		adb shell input keyevent KEYCODE_WAKEUP &&
		# Drop keep-alive connections that may point at a previous mapping.
		adb shell am force-stop "$CHROME" &&
		adb shell am start -a android.intent.action.VIEW -d "$URL" "$CHROME" >/dev/null &&
		sleep 8 &&
		adb shell input tap "$TAP_X" "$TAP_Y" && return
	echo "palma setup failed; retrying on the next poll"
	adb reverse --remove "tcp:${PORT}" 2>/dev/null
}

while true; do
	if [ "$(adb get-state 2>/dev/null)" = device ] && ! has_reverse; then
		setup
	fi
	sleep 30
done
