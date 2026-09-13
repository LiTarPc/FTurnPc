#pragma once
void wdtt_tray_init(const char *icon_path);
void wdtt_tray_set_visible(int visible);
void wdtt_tray_set_status(int connected, long long rx, long long tx, int workers);
void wdtt_gtk_main(void);
