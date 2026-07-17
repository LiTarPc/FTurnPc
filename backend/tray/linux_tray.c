#include "linux_tray.h"
#include <libayatana-appindicator/app-indicator.h>
#include <gtk/gtk.h>
#include <stdio.h>
#include <libgen.h>
#include <string.h>

static AppIndicator *indicator = NULL;
static GtkWidget *menu = NULL;
static GtkWidget *status_label = NULL;

extern void onShowClicked();
extern void onQuitClicked();

static void show_cb(GtkMenuItem *item, gpointer data) { onShowClicked(); }
static void quit_cb(GtkMenuItem *item, gpointer data) { onQuitClicked(); }

static GtkWidget *make_markup_item(GtkWidget **out_label, const char *markup) {
    GtkWidget *item = gtk_menu_item_new();
    GtkWidget *lbl  = gtk_label_new(NULL);
    gtk_label_set_markup(GTK_LABEL(lbl), markup);
    gtk_label_set_xalign(GTK_LABEL(lbl), 0.0f);
    gtk_container_add(GTK_CONTAINER(item), lbl);
    if (out_label) *out_label = lbl;
    return item;
}

void wdtt_tray_init(const char *icon_path) {
    gtk_init(NULL, NULL);

    char *path_copy = strdup(icon_path);
    char *dir  = strdup(dirname(path_copy));
    free(path_copy);

    path_copy = strdup(icon_path);
    char *dot = strrchr(basename(path_copy), '.');
    if (dot) *dot = '\0';
    char *name = strdup(basename(path_copy));
    free(path_copy);

#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wdeprecated-declarations"
    indicator = app_indicator_new_with_path("pwdtt", name, APP_INDICATOR_CATEGORY_APPLICATION_STATUS, dir);
#pragma GCC diagnostic pop
    app_indicator_set_status(indicator, APP_INDICATOR_STATUS_ACTIVE);

    free(dir);
    free(name);

    menu = gtk_menu_new();

    GtkWidget *status_item = make_markup_item(&status_label,
        "<span color='#cc3333'>\xe2\x97\x8f</span> \xd0\x9e\xd1\x82\xd0\xba\xd0\xbb\xd1\x8e\xd1\x87\xd0\xb5\xd0\xbd\xd0\xbe");
    gtk_menu_shell_append(GTK_MENU_SHELL(menu), status_item);

    gtk_menu_shell_append(GTK_MENU_SHELL(menu), gtk_separator_menu_item_new());

    GtkWidget *show_item = gtk_menu_item_new_with_label(
        "\xd0\x9f\xd0\xbe\xd0\xba\xd0\xb0\xd0\xb7\xd0\xb0\xd1\x82\xd1\x8c");
    g_signal_connect(show_item, "activate", G_CALLBACK(show_cb), NULL);
    gtk_menu_shell_append(GTK_MENU_SHELL(menu), show_item);

    gtk_menu_shell_append(GTK_MENU_SHELL(menu), gtk_separator_menu_item_new());

    GtkWidget *quit_item = gtk_menu_item_new_with_label(
        "\xd0\x92\xd1\x8b\xd1\x85\xd0\xbe\xd0\xb4");
    g_signal_connect(quit_item, "activate", G_CALLBACK(quit_cb), NULL);
    gtk_menu_shell_append(GTK_MENU_SHELL(menu), quit_item);

    gtk_widget_show_all(menu);
    app_indicator_set_menu(indicator, GTK_MENU(menu));
}

static void update_status_label(int connected, long long rx, long long tx, int workers) {
    if (!status_label) return;
    char buf[256];
    if (connected) {
        double total_mb = (double)(rx + tx) / (1024.0 * 1024.0);
        snprintf(buf, sizeof(buf),
            "<span color='#33cc33'>\xe2\x97\x8f</span>"
            " \xd0\x9f\xd0\xbe\xd0\xb4\xd0\xba\xd0\xbb\xd1\x8e\xd1\x87\xd0\xb5\xd0\xbd\xd0\xbe"
            "  \xe2\x86\x91%.1f \xd0\x9c\xd0\x91"
            "  \xd0\xb2\xd0\xbe\xd1\x80\xd0\xba: %d",
            total_mb, workers);
    } else {
        snprintf(buf, sizeof(buf),
            "<span color='#cc3333'>\xe2\x97\x8f</span>"
            " \xd0\x9e\xd1\x82\xd0\xba\xd0\xbb\xd1\x8e\xd1\x87\xd0\xb5\xd0\xbd\xd0\xbe");
    }
    gtk_label_set_markup(GTK_LABEL(status_label), buf);
}

typedef struct { int connected; long long rx, tx; int workers; } StatusArgs;

static gboolean do_update_status(gpointer data) {
    StatusArgs *a = (StatusArgs *)data;
    update_status_label(a->connected, a->rx, a->tx, a->workers);
    g_free(a);
    return FALSE;
}

void wdtt_tray_set_status(int connected, long long rx, long long tx, int workers) {
    StatusArgs *a = g_new(StatusArgs, 1);
    a->connected = connected; a->rx = rx; a->tx = tx; a->workers = workers;
    g_idle_add(do_update_status, a);
}

void wdtt_tray_set_visible(int visible) {
    if (indicator == NULL) return;
    app_indicator_set_status(indicator,
        visible ? APP_INDICATOR_STATUS_ACTIVE : APP_INDICATOR_STATUS_PASSIVE);
}

void wdtt_gtk_main(void) {
    gtk_main();
}
