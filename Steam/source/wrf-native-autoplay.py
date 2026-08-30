#!/usr/bin/env python3
"""Start WRF from an already-open native MY.GAMES launcher window."""

from __future__ import annotations

import argparse
import os
import subprocess
import tempfile
import time
from pathlib import Path

from PIL import Image
from Xlib import X, XK, display, protocol


LAUNCHER_TITLE = "MY.GAMES Launcher"


def windows() -> list[tuple[str, int, str, str]]:
    output = subprocess.run(
        ["wmctrl", "-lpGx"], check=True, capture_output=True, text=True
    ).stdout
    found = []
    for line in output.splitlines():
        fields = line.split(maxsplit=9)
        if len(fields) == 10:
            found.append((fields[0], int(fields[2]), fields[7], fields[9]))
    return found


def wait_window(predicate, timeout: float) -> tuple[str, int, str, str]:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        for item in windows():
            if predicate(item):
                return item
        time.sleep(0.5)
    raise TimeoutError("matching window not found")


def capture(window_id: str, output: Path) -> Image.Image | None:
    try:
        subprocess.run(
            ["import", "-window", window_id, str(output)],
            check=True,
            capture_output=True,
        )
    except subprocess.CalledProcessError:
        return None
    image = Image.open(output)
    image.load()
    return image.convert("RGB")


def window_size(window_id: str) -> tuple[int, int]:
    connection = display.Display()
    window = connection.create_resource_object("window", int(window_id, 16))
    geometry = window.get_geometry()
    connection.close()
    return geometry.width, geometry.height


def is_game_window(item: tuple[str, int, str, str]) -> bool:
    if "war robots" not in item[3].lower() and "frontiers" not in item[3].lower():
        return False
    try:
        command_line = Path(f"/proc/{item[1]}/cmdline").read_bytes()
    except OSError:
        return False
    return b"WRFrontiers-Win64-Shipping.exe" in command_line


def has_top_right_play_button(image: Image.Image) -> bool:
    width, height = image.size
    center = image.getpixel((round(width * 0.893), round(height * 0.105)))
    if not is_play_blue(center):
        return False
    blue = 0
    for pixel in image.crop(
        (round(width * 0.75), round(height * 0.04), width, round(height * 0.18))
    ).get_flattened_data():
        if is_play_blue(pixel):
            blue += 1
    return blue >= max(2500, width * height // 500)


def is_play_blue(pixel: tuple[int, int, int]) -> bool:
    red, green, blue = pixel
    return red < 100 and 65 < green < 185 and blue > 185 and blue > green + 55


def send_button(window_id: str, x: int, y: int) -> None:
    connection = display.Display()
    window = connection.create_resource_object("window", int(window_id, 16))
    root = connection.screen().root
    for event_type, mask in (
        (protocol.event.ButtonPress, X.ButtonPressMask),
        (protocol.event.ButtonRelease, X.ButtonReleaseMask),
    ):
        event = event_type(
            time=X.CurrentTime,
            root=root,
            window=window,
            same_screen=1,
            child=X.NONE,
            root_x=0,
            root_y=0,
            event_x=x,
            event_y=y,
            state=0,
            detail=1,
        )
        window.send_event(event, propagate=True, event_mask=mask)
    connection.sync()
    connection.close()


def send_space(window_id: str) -> None:
    connection = display.Display()
    window = connection.create_resource_object("window", int(window_id, 16))
    root = connection.screen().root
    keycode = connection.keysym_to_keycode(XK.string_to_keysym("space"))
    for event_type, mask in (
        (protocol.event.KeyPress, X.KeyPressMask),
        (protocol.event.KeyRelease, X.KeyReleaseMask),
    ):
        event = event_type(
            time=X.CurrentTime,
            root=root,
            window=window,
            same_screen=1,
            child=X.NONE,
            root_x=0,
            root_y=0,
            event_x=0,
            event_y=0,
            state=0,
            detail=keycode,
        )
        window.send_event(event, propagate=True, event_mask=mask)
    connection.sync()
    connection.close()


def self_test() -> None:
    image = Image.new("RGB", (1266, 768), "black")
    for x in range(1020, 1240):
        for y in range(60, 103):
            image.putpixel((x, y), (5, 102, 244))
    assert has_top_right_play_button(image)
    assert not has_top_right_play_button(Image.new("RGB", (1266, 768), "black"))


def skip_intro(game: tuple[str, int, str, str]) -> None:
    subprocess.run(["wmctrl", "-ia", game[0]], check=True)
    for _ in range(7):
        subprocess.run(["wmctrl", "-ia", game[0]], check=True)
        time.sleep(0.1)
        send_space(game[0])
        time.sleep(0.9)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--launcher-timeout", type=float, default=120)
    parser.add_argument("--game-timeout", type=float, default=180)
    parser.add_argument("--game-only", action="store_true")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
        return 0
    if args.game_only:
        game = wait_window(is_game_window, args.game_timeout)
        skip_intro(game)
        print(f"game_window={game[0]} intro_spaces=7")
        return 0

    launcher = wait_window(
        lambda item: item[3].endswith(LAUNCHER_TITLE), args.launcher_timeout
    )
    launcher_id = launcher[0]
    existing_window_ids = {item[0] for item in windows()}
    subprocess.run(["wmctrl", "-ia", launcher_id], check=True)

    with tempfile.TemporaryDirectory(prefix="wrf-native-autoplay-") as temp:
        screenshot = Path(temp) / "launcher.png"
        image = capture(launcher_id, screenshot)
        for _ in range(5):
            if image is None:
                width, height = window_size(launcher_id)
                send_button(
                    launcher_id,
                    round(width * 0.105),
                    round(height * 0.353),
                )
                time.sleep(3)
                break
            if has_top_right_play_button(image):
                break
            send_button(
                launcher_id,
                round(image.width * 0.105),
                round(image.height * 0.353),
            )
            time.sleep(2)
            image = capture(launcher_id, screenshot)
        else:
            raise RuntimeError("Frontiers page Play button was not visually confirmed")

        game_deadline = time.monotonic() + args.game_timeout
        next_click = 0.0
        play_clicks = 0
        candidate_since: dict[str, float] = {}
        game = None
        while game is None and time.monotonic() < game_deadline:
            now = time.monotonic()
            current_windows = windows()
            candidates = [
                item
                for item in current_windows
                if item[0] not in existing_window_ids
                and is_game_window(item)
            ]
            current_ids = {item[0] for item in candidates}
            candidate_since = {
                window_id: first_seen
                for window_id, first_seen in candidate_since.items()
                if window_id in current_ids
            }
            for item in candidates:
                candidate_since.setdefault(item[0], now)
                if now - candidate_since[item[0]] >= 3:
                    game = item
                    break
            if game is None and play_clicks < 2 and now >= next_click:
                image = capture(launcher_id, screenshot)
                if image is None or has_top_right_play_button(image):
                    width, height = window_size(launcher_id) if image is None else image.size
                    subprocess.run(["wmctrl", "-ia", launcher_id], check=True)
                    send_button(
                        launcher_id,
                        round(width * 0.893),
                        round(height * 0.105),
                    )
                    play_clicks += 1
                    print("play_click=sent", flush=True)
                next_click = now + 3
            time.sleep(0.5)
        if game is None:
            raise TimeoutError("game window not found after clicking Play")

    skip_intro(game)
    print(f"launcher_window={launcher_id} game_window={game[0]} intro_spaces=7")
    return 0


if __name__ == "__main__":
    os.umask(0o077)
    raise SystemExit(main())
