/* open-peering.js - live Open Peering MAC distribution view. */
(function (w) {
  "use strict";
  var K = w.K, I = w.I18N, h = K.h, V = w.Views;
  var viewMode = "members";

  function qualityBadge(direction) {
    var quality = direction.quality || "unavailable";
    var cls = quality === "ok" ? "badge--calm" : quality === "partial" ? "badge--elev" : "badge--muted";
    return K.badge(cls, I.t("peer.quality." + quality), quality === "ok" ? "check" : "info");
  }

  function aggregate(direction, label, cssClass) {
    return h("div", { class: "peer-total " + cssClass }, [
      h("div", { class: "peer-total__label", text: label }),
      h("div", { class: "peer-total__rate", text: I.mbps(direction.mbps || 0) }),
      h("div", { class: "peer-total__pps", text: I.pps(direction.pps || 0) })
    ]);
  }

  function macRows(rows, cssClass) {
    rows = rows || [];
    if (!rows.length) {
      return K.empty("activity", I.t("peer.empty.title"), I.t("peer.empty.sub"), "muted");
    }
    return h("div", { class: "peer-list" }, rows.map(function (peer, index) {
      var share = Math.max(0, Math.min(1, peer.share || 0));
      var ips = peer.ips || [];
      var macLabel = peer.mac + (ips.length ? " (" + ips.join(", ") + ")" : "");
      return h("div", { class: "peer-row" }, [
        h("span", { class: "peer-row__rank", text: String(index + 1) }),
        h("span", { class: "peer-row__mac mono", text: macLabel, attrs: { title: macLabel } }),
        h("span", { class: "peer-row__rate", text: I.mbps(peer.mbps || 0) }),
        h("span", { class: "peer-row__pps", text: I.pps(peer.pps || 0) }),
        h("span", { class: "peer-row__pct", text: I.pct(share) }),
        h("progress", { class: "peer-row__bar " + cssClass, attrs: {
          max: "100", value: String(share * 100), "aria-label": macLabel + " " + I.pct(share)
        } })
      ]);
    }));
  }

  function directionCard(direction, label, icon, cssClass) {
    var unknown = direction.unknown_share || 0;
    return h("div", { class: "card peer-card" }, [
      h("div", { class: "card__head" }, [
        h("div", { class: "card__title" }, [w.icon(icon), h("span", { text: label })]),
        qualityBadge(direction)
      ]),
      h("div", { class: "card__body" }, [
        h("div", { class: "peer-card__summary" }, [
          h("strong", { text: I.mbps(direction.mbps || 0) }),
          h("span", { text: I.pps(direction.pps || 0) }),
          h("span", { text: I.t("peer.distinct", { count: I.num(direction.distinct_macs || 0) }) })
        ]),
        unknown > 0 ? h("div", { class: "peer-unknown" }, [
          w.icon("info"), h("span", { text: I.t("peer.unknown", { share: I.pct(unknown) }) })
        ]) : null,
        macRows(direction.all_macs || direction.top_macs, cssClass)
      ])
    ]);
  }

  function memberDirection(direction, label, cssClass) {
    direction = direction || { top_macs: [] };
    return h("div", { class: "peer-member__direction" }, [
      h("div", { class: "peer-member__direction-head" }, [
        h("strong", { text: label }),
        h("span", { text: I.mbps(direction.mbps || 0) + " · " + I.pps(direction.pps || 0) }),
        h("small", { text: I.t("peer.distinct", { count: I.num(direction.distinct_macs || 0) }) })
      ]),
      macRows(direction.top_macs, cssClass)
    ]);
  }

  function groupedMembers(ingress, egress) {
    var byName = {};
    (ingress.members || []).forEach(function (member) {
      byName[member.name] = byName[member.name] || { name: member.name };
      byName[member.name].ingress = member;
    });
    (egress.members || []).forEach(function (member) {
      byName[member.name] = byName[member.name] || { name: member.name };
      byName[member.name].egress = member;
    });
    var members = Object.keys(byName).map(function (name) { return byName[name]; });
    members.sort(function (a, b) {
      var aMbps = (a.ingress ? a.ingress.mbps : 0) + (a.egress ? a.egress.mbps : 0);
      var bMbps = (b.ingress ? b.ingress.mbps : 0) + (b.egress ? b.egress.mbps : 0);
      return bMbps === aMbps ? a.name.localeCompare(b.name) : bMbps - aMbps;
    });
    return h("div", { class: "peer-members" }, members.map(function (member) {
      return h("section", { class: "card peer-member" }, [
        h("div", { class: "card__head" }, [
          h("div", { class: "card__title" }, [w.icon("activity"), h("span", { text: member.name })]),
          h("span", { class: "peer-member__total", text:
            I.mbps((member.ingress ? member.ingress.mbps : 0) + (member.egress ? member.egress.mbps : 0)) })
        ]),
        h("div", { class: "card__body peer-member__grid" }, [
          memberDirection(member.ingress, I.t("peer.ingress") + " · " + I.t("peer.top10"), "is-ingress"),
          memberDirection(member.egress, I.t("peer.egress") + " · " + I.t("peer.top10"), "is-egress")
        ])
      ]);
    }));
  }

  function openPeering(root, ctx) {
    var data = ctx.openPeering || {};
    var ingress = data.ingress || { top_macs: [], quality: "unavailable" };
    var egress = data.egress || { top_macs: [], quality: "unavailable" };
    var modeSwitch = h("div", { class: "seg", attrs: { role: "group", "aria-label": I.t("nav.open_peering") } }, [
      h("button", { class: "seg__btn" + (viewMode === "members" ? " is-on" : ""), text: I.t("peer.by_member"),
        attrs: { "aria-pressed": String(viewMode === "members") }, onclick: function () { viewMode = "members"; openPeering(root, ctx); } }),
      h("button", { class: "seg__btn" + (viewMode === "all" ? " is-on" : ""), text: I.t("peer.all_macs"),
        attrs: { "aria-pressed": String(viewMode === "all") }, onclick: function () { viewMode = "all"; openPeering(root, ctx); } })
    ]);
    K.mount(root, [
      V.viewHead(I.t("nav.open_peering"), I.t("peer.headline"), [modeSwitch]),
      h("div", { class: "peer-aggregate" }, [
        h("div", { class: "peer-aggregate__title" }, [
          w.icon("activity"), h("span", { text: I.t("peer.aggregate") }),
          h("small", { text: I.t("peer.window", { seconds: data.window_seconds || 0 }) })
        ]),
        h("div", { class: "peer-totals" }, [
          aggregate(ingress, I.t("peer.ingress"), "is-ingress"),
          aggregate(egress, I.t("peer.egress"), "is-egress")
        ])
      ]),
      viewMode === "members" ? groupedMembers(ingress, egress) : h("div", { class: "peer-grid" }, [
        directionCard(ingress, I.t("peer.ingress_all"), "arrow-down", "is-ingress"),
        directionCard(egress, I.t("peer.egress_all"), "arrow-up", "is-egress")
      ])
    ]);
  }

  V["open-peering"] = openPeering;
})(window);
