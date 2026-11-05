//#region \0vite/modulepreload-polyfill.js
(function polyfill() {
	const relList = document.createElement("link").relList;
	if (relList && relList.supports && relList.supports("modulepreload")) return;
	for (const link of document.querySelectorAll("link[rel=\"modulepreload\"]")) processPreload(link);
	new MutationObserver((mutations) => {
		for (const mutation of mutations) {
			if (mutation.type !== "childList") continue;
			for (const node of mutation.addedNodes) if (node.tagName === "LINK" && node.rel === "modulepreload") processPreload(node);
		}
	}).observe(document, {
		childList: true,
		subtree: true
	});
	function getFetchOpts(link) {
		const fetchOpts = {};
		if (link.integrity) fetchOpts.integrity = link.integrity;
		if (link.referrerPolicy) fetchOpts.referrerPolicy = link.referrerPolicy;
		if (link.crossOrigin === "use-credentials") fetchOpts.credentials = "include";
		else if (link.crossOrigin === "anonymous") fetchOpts.credentials = "omit";
		else fetchOpts.credentials = "same-origin";
		return fetchOpts;
	}
	function processPreload(link) {
		if (link.ep) return;
		link.ep = true;
		const fetchOpts = getFetchOpts(link);
		fetch(link.href, fetchOpts);
	}
})();
//#endregion
//#region node_modules/preact/dist/preact.module.js
var n$1;
var l$3;
var u$3;
var t$3;
var i$3;
var r$2;
var o$2;
var e$2;
var f$3;
var c$2;
var a$2;
var s$2;
var h$3;
var p$3;
var v$2;
var d$3 = {};
var w$3 = [];
var _$2 = /acit|ex(?:s|g|n|p|$)|rph|grid|ows|mnc|ntw|ine[ch]|zoo|^ord|itera/i;
var g$3 = Array.isArray;
function m$2(n, l) {
	for (var u in l) n[u] = l[u];
	return n;
}
function b$2(n) {
	n && n.parentNode && n.parentNode.removeChild(n);
}
function k$1(l, u, t) {
	var i, r, o, e = {};
	for (o in u) "key" == o ? i = u[o] : "ref" == o ? r = u[o] : e[o] = u[o];
	if (arguments.length > 2 && (e.children = arguments.length > 3 ? n$1.call(arguments, 2) : t), "function" == typeof l && null != l.defaultProps) for (o in l.defaultProps) void 0 === e[o] && (e[o] = l.defaultProps[o]);
	return x$2(l, e, i, r, null);
}
function x$2(n, t, i, r, o) {
	var e = {
		type: n,
		props: t,
		key: i,
		ref: r,
		__k: null,
		__: null,
		__b: 0,
		__e: null,
		__c: null,
		constructor: void 0,
		__v: null == o ? ++u$3 : o,
		__i: -1,
		__u: 0
	};
	return null == o && null != l$3.vnode && l$3.vnode(e), e;
}
function S$1(n) {
	return n.children;
}
function C$1(n, l) {
	this.props = n, this.context = l;
}
function $(n, l) {
	if (null == l) return n.__ ? $(n.__, n.__i + 1) : null;
	for (var u; l < n.__k.length; l++) if (null != (u = n.__k[l]) && null != u.__e) return u.__e;
	return "function" == typeof n.type ? $(n) : null;
}
function I(n) {
	if (n.__P && n.__d) {
		var u = n.__v, t = u.__e, i = [], r = [], o = m$2({}, u);
		o.__v = u.__v + 1, l$3.vnode && l$3.vnode(o), q$2(n.__P, o, u, n.__n, n.__P.namespaceURI, 32 & u.__u ? [t] : null, i, null == t ? $(u) : t, !!(32 & u.__u), r), o.__v = u.__v, o.__.__k[o.__i] = o, D$1(i, o, r), u.__e = u.__ = null, o.__e != t && P(o);
	}
}
function P(n) {
	if (null != (n = n.__) && null != n.__c) return n.__e = n.__c.base = null, n.__k.some(function(l) {
		if (null != l && null != l.__e) return n.__e = n.__c.base = l.__e;
	}), P(n);
}
function A$1(n) {
	(!n.__d && (n.__d = !0) && i$3.push(n) && !H.__r++ || r$2 != l$3.debounceRendering) && ((r$2 = l$3.debounceRendering) || o$2)(H);
}
function H() {
	try {
		for (var n, l = 1; i$3.length;) i$3.length > l && i$3.sort(e$2), n = i$3.shift(), l = i$3.length, I(n);
	} finally {
		i$3.length = H.__r = 0;
	}
}
function L(n, l, u, t, i, r, o, e, f, c, a) {
	var s, h, p, v, y, _, g, m = t && t.__k || w$3, b = l.length;
	for (f = T$1(u, l, m, f, b), s = 0; s < b; s++) null != (p = u.__k[s]) && (h = -1 != p.__i && m[p.__i] || d$3, p.__i = s, _ = q$2(n, p, h, i, r, o, e, f, c, a), v = p.__e, p.ref && h.ref != p.ref && (h.ref && J(h.ref, null, p), a.push(p.ref, p.__c || v, p)), null == y && null != v && (y = v), (g = !!(4 & p.__u)) || h.__k === p.__k ? (f = j$2(p, f, n, g), g && h.__e && (h.__e = null)) : "function" == typeof p.type && void 0 !== _ ? f = _ : v && (f = v.nextSibling), p.__u &= -7);
	return u.__e = y, f;
}
function T$1(n, l, u, t, i) {
	var r, o, e, f, c, a = u.length, s = a, h = 0;
	for (n.__k = new Array(i), r = 0; r < i; r++) null != (o = l[r]) && "boolean" != typeof o && "function" != typeof o ? ("string" == typeof o || "number" == typeof o || "bigint" == typeof o || o.constructor == String ? o = n.__k[r] = x$2(null, o, null, null, null) : g$3(o) ? o = n.__k[r] = x$2(S$1, { children: o }, null, null, null) : void 0 === o.constructor && o.__b > 0 ? o = n.__k[r] = x$2(o.type, o.props, o.key, o.ref ? o.ref : null, o.__v) : n.__k[r] = o, f = r + h, o.__ = n, o.__b = n.__b + 1, e = null, -1 != (c = o.__i = O(o, u, f, s)) && (s--, (e = u[c]) && (e.__u |= 2)), null == e || null == e.__v ? (-1 == c && (i > a ? h-- : i < a && h++), "function" != typeof o.type && (o.__u |= 4)) : c != f && (c == f - 1 ? h-- : c == f + 1 ? h++ : (c > f ? h-- : h++, o.__u |= 4))) : n.__k[r] = null;
	if (s) for (r = 0; r < a; r++) null != (e = u[r]) && 0 == (2 & e.__u) && (e.__e == t && (t = $(e)), K(e, e));
	return t;
}
function j$2(n, l, u, t) {
	var i, r;
	if ("function" == typeof n.type) {
		for (i = n.__k, r = 0; i && r < i.length; r++) i[r] && (i[r].__ = n, l = j$2(i[r], l, u, t));
		return l;
	}
	n.__e != l && (t && (l && n.type && !l.parentNode && (l = $(n)), u.insertBefore(n.__e, l || null)), l = n.__e);
	do
		l = l && l.nextSibling;
	while (null != l && 8 == l.nodeType);
	return l;
}
function O(n, l, u, t) {
	var i, r, o, e = n.key, f = n.type, c = l[u], a = null != c && 0 == (2 & c.__u);
	if (null === c && null == e || a && e == c.key && f == c.type) return u;
	if (t > (a ? 1 : 0)) {
		for (i = u - 1, r = u + 1; i >= 0 || r < l.length;) if (null != (c = l[o = i >= 0 ? i-- : r++]) && 0 == (2 & c.__u) && e == c.key && f == c.type) return o;
	}
	return -1;
}
function z$1(n, l, u) {
	"-" == l[0] ? n.setProperty(l, null == u ? "" : u) : n[l] = null == u ? "" : "number" != typeof u || _$2.test(l) ? u : u + "px";
}
function N(n, l, u, t, i) {
	var r, o;
	n: if ("style" == l) if ("string" == typeof u) n.style.cssText = u;
	else {
		if ("string" == typeof t && (n.style.cssText = t = ""), t) for (l in t) u && l in u || z$1(n.style, l, "");
		if (u) for (l in u) t && u[l] == t[l] || z$1(n.style, l, u[l]);
	}
	else if ("o" == l[0] && "n" == l[1]) r = l != (l = l.replace(s$2, "$1")), o = l.toLowerCase(), l = o in n || "onFocusOut" == l || "onFocusIn" == l ? o.slice(2) : l.slice(2), n.l || (n.l = {}), n.l[l + r] = u, u ? t ? u[a$2] = t[a$2] : (u[a$2] = h$3, n.addEventListener(l, r ? v$2 : p$3, r)) : n.removeEventListener(l, r ? v$2 : p$3, r);
	else {
		if ("http://www.w3.org/2000/svg" == i) l = l.replace(/xlink(H|:h)/, "h").replace(/sName$/, "s");
		else if ("width" != l && "height" != l && "href" != l && "list" != l && "form" != l && "tabIndex" != l && "download" != l && "rowSpan" != l && "colSpan" != l && "role" != l && "popover" != l && l in n) try {
			n[l] = null == u ? "" : u;
			break n;
		} catch (n) {}
		"function" == typeof u || (null == u || !1 === u && "-" != l[4] ? n.removeAttribute(l) : n.setAttribute(l, "popover" == l && 1 == u ? "" : u));
	}
}
function V(n) {
	return function(u) {
		if (this.l) {
			var t = this.l[u.type + n];
			if (null == u[c$2]) u[c$2] = h$3++;
			else if (u[c$2] < t[a$2]) return;
			return t(l$3.event ? l$3.event(u) : u);
		}
	};
}
function q$2(n, u, t, i, r, o, e, f, c, a) {
	var s, h, p, v, y, d, _, k, x, M, $, I, P, A, H, T, j = u.type;
	if (void 0 !== u.constructor) return null;
	128 & t.__u && (c = !!(32 & t.__u), o = [f = u.__e = t.__e]), (s = l$3.__b) && s(u);
	n: if ("function" == typeof j) {
		h = e.length;
		try {
			if (x = u.props, M = j.prototype && j.prototype.render, $ = (s = j.contextType) && i[s.__c], I = s ? $ ? $.props.value : s.__ : i, t.__c ? k = (p = u.__c = t.__c).__ = p.__E : (M ? u.__c = p = new j(x, I) : (u.__c = p = new C$1(x, I), p.constructor = j, p.render = Q), $ && $.sub(p), p.state || (p.state = {}), p.__n = i, v = p.__d = !0, p.__h = [], p._sb = []), M && null == p.__s && (p.__s = p.state), M && null != j.getDerivedStateFromProps && (p.__s == p.state && (p.__s = m$2({}, p.__s)), m$2(p.__s, j.getDerivedStateFromProps(x, p.__s))), y = p.props, d = p.state, p.__v = u, v) M && null == j.getDerivedStateFromProps && null != p.componentWillMount && p.componentWillMount(), M && null != p.componentDidMount && p.__h.push(p.componentDidMount);
			else {
				if (M && null == j.getDerivedStateFromProps && x !== y && null != p.componentWillReceiveProps && p.componentWillReceiveProps(x, I), u.__v == t.__v || !p.__e && null != p.shouldComponentUpdate && !1 === p.shouldComponentUpdate(x, p.__s, I)) {
					u.__v != t.__v && (p.props = x, p.state = p.__s, p.__d = !1), u.__e = t.__e, u.__k = t.__k, u.__k.some(function(n) {
						n && (n.__ = u);
					}), w$3.push.apply(p.__h, p._sb), p._sb = [], p.__h.length && e.push(p);
					break n;
				}
				null != p.componentWillUpdate && p.componentWillUpdate(x, p.__s, I), M && null != p.componentDidUpdate && p.__h.push(function() {
					p.componentDidUpdate(y, d, _);
				});
			}
			if (p.context = I, p.props = x, p.__P = n, p.__e = !1, P = l$3.__r, A = 0, M) p.state = p.__s, p.__d = !1, P && P(u), s = p.render(p.props, p.state, p.context), w$3.push.apply(p.__h, p._sb), p._sb = [];
			else do
				p.__d = !1, P && P(u), s = p.render(p.props, p.state, p.context), p.state = p.__s;
			while (p.__d && ++A < 25);
			p.state = p.__s, null != p.getChildContext && (i = m$2(m$2({}, i), p.getChildContext())), M && !v && null != p.getSnapshotBeforeUpdate && (_ = p.getSnapshotBeforeUpdate(y, d)), H = null != s && s.type === S$1 && null == s.key ? E$1(s.props.children) : s, f = L(n, g$3(H) ? H : [H], u, t, i, r, o, e, f, c, a), p.base = u.__e, u.__u &= -161, p.__h.length && e.push(p), k && (p.__E = p.__ = null);
		} catch (n) {
			if (e.length = h, u.__v = null, c || null != o) {
				if (n.then) {
					for (u.__u |= c ? 160 : 128; f && 8 == f.nodeType && f.nextSibling;) f = f.nextSibling;
					null != o && (o[o.indexOf(f)] = null), u.__e = f;
				} else if (null != o) for (T = o.length; T--;) b$2(o[T]);
			} else u.__e = t.__e;
			u.__k ??= t.__k || [], n.then || B$1(u), l$3.__e(n, u, t);
		}
	} else null == o && u.__v == t.__v ? (u.__k = t.__k, u.__e = t.__e) : f = u.__e = G(t.__e, u, t, i, r, o, e, c, a);
	return (s = l$3.diffed) && s(u), 128 & u.__u ? void 0 : f;
}
function B$1(n) {
	n && (n.__c && (n.__c.__e = !0), n.__k && n.__k.some(B$1));
}
function D$1(n, u, t) {
	for (var i = 0; i < t.length; i++) J(t[i], t[++i], t[++i]);
	l$3.__c && l$3.__c(u, n), n.some(function(u) {
		try {
			n = u.__h, u.__h = [], n.some(function(n) {
				n.call(u);
			});
		} catch (n) {
			l$3.__e(n, u.__v);
		}
	});
}
function E$1(n) {
	return "object" != typeof n || null == n || n.__b > 0 ? n : g$3(n) ? n.map(E$1) : void 0 !== n.constructor ? null : m$2({}, n);
}
function G(u, t, i, r, o, e, f, c, a) {
	var s, h, p, v, y, w, _, m = i.props || d$3, k = t.props, x = t.type;
	if ("svg" == x ? o = "http://www.w3.org/2000/svg" : "math" == x ? o = "http://www.w3.org/1998/Math/MathML" : o || (o = "http://www.w3.org/1999/xhtml"), null != e) {
		for (s = 0; s < e.length; s++) if ((y = e[s]) && "setAttribute" in y == !!x && (x ? y.localName == x : 3 == y.nodeType)) {
			u = y, e[s] = null;
			break;
		}
	}
	if (null == u) {
		if (null == x) return document.createTextNode(k);
		u = document.createElementNS(o, x, k.is && k), c && (l$3.__m && l$3.__m(t, e), c = !1), e = null;
	}
	if (null == x) m === k || c && u.data == k || (u.data = k);
	else {
		if (e = "textarea" == x && null != k.defaultValue ? null : e && n$1.call(u.childNodes), !c && null != e) for (m = {}, s = 0; s < u.attributes.length; s++) m[(y = u.attributes[s]).name] = y.value;
		for (s in m) y = m[s], "dangerouslySetInnerHTML" == s ? p = y : "children" == s || s in k || "value" == s && "defaultValue" in k || "checked" == s && "defaultChecked" in k || N(u, s, null, y, o);
		for (s in k) y = k[s], "children" == s ? v = y : "dangerouslySetInnerHTML" == s ? h = y : "value" == s ? w = y : "checked" == s ? _ = y : c && "function" != typeof y || m[s] === y || N(u, s, y, m[s], o);
		if (h) c || p && (h.__html == p.__html || h.__html == u.innerHTML) || (u.innerHTML = h.__html), t.__k = [];
		else if (p && (u.innerHTML = ""), L("template" == t.type ? u.content : u, g$3(v) ? v : [v], t, i, r, "foreignObject" == x ? "http://www.w3.org/1999/xhtml" : o, e, f, e ? e[0] : i.__k && $(i, 0), c, a), null != e) for (s = e.length; s--;) b$2(e[s]);
		c && "textarea" != x || (s = "value", "progress" == x && null == w ? u.removeAttribute("value") : null != w && (w !== u[s] || "progress" == x && !w || "option" == x && w != m[s]) && N(u, s, w, m[s], o), s = "checked", null != _ && _ != u[s] && N(u, s, _, m[s], o));
	}
	return u;
}
function J(n, u, t) {
	try {
		if ("function" == typeof n) {
			var i = "function" == typeof n.__u;
			i && n.__u(), i && null == u || (n.__u = n(u));
		} else n.current = u;
	} catch (n) {
		l$3.__e(n, t);
	}
}
function K(n, u, t) {
	var i, r;
	if (l$3.unmount && l$3.unmount(n), (i = n.ref) && (i.current && i.current != n.__e || J(i, null, u)), null != (i = n.__c)) {
		if (i.componentWillUnmount) try {
			i.componentWillUnmount();
		} catch (n) {
			l$3.__e(n, u);
		}
		i.base = i.__P = i.__n = null;
	}
	if (i = n.__k) for (r = 0; r < i.length; r++) i[r] && K(i[r], u, t || "function" != typeof n.type);
	t || b$2(n.__e), n.__c = n.__ = n.__e = void 0;
}
function Q(n, l, u) {
	return this.constructor(n, u);
}
function R(u, t, i) {
	var r, o, e, f;
	t == document && (t = document.documentElement), l$3.__ && l$3.__(u, t), o = (r = "function" == typeof i) ? null : i && i.__k || t.__k, e = [], f = [], q$2(t, u = (!r && i || t).__k = k$1(S$1, null, [u]), o || d$3, d$3, t.namespaceURI, !r && i ? [i] : o ? null : t.firstChild ? n$1.call(t.childNodes) : null, e, !r && i ? i : o ? o.__e : t.firstChild, r, f), D$1(e, u, f), u.props.children = null;
}
n$1 = w$3.slice, l$3 = { __e: function(n, l, u, t) {
	for (var i, r, o; l = l.__;) if ((i = l.__c) && !i.__) try {
		if ((r = i.constructor) && null != r.getDerivedStateFromError && (i.setState(r.getDerivedStateFromError(n)), o = i.__d), null != i.componentDidCatch && (i.componentDidCatch(n, t || {}), o = i.__d), o) return i.__E = i;
	} catch (l) {
		n = l;
	}
	throw n;
} }, u$3 = 0, t$3 = function(n) {
	return null != n && void 0 === n.constructor;
}, C$1.prototype.setState = function(n, l) {
	var u = null != this.__s && this.__s != this.state ? this.__s : this.__s = m$2({}, this.state);
	"function" == typeof n && (n = n(m$2({}, u), this.props)), n && m$2(u, n), null != n && this.__v && (l && this._sb.push(l), A$1(this));
}, C$1.prototype.forceUpdate = function(n) {
	this.__v && (this.__e = !0, n && this.__h.push(n), A$1(this));
}, C$1.prototype.render = S$1, i$3 = [], o$2 = "function" == typeof Promise ? Promise.prototype.then.bind(Promise.resolve()) : setTimeout, e$2 = function(n, l) {
	return n.__v.__b - l.__v.__b;
}, H.__r = 0, f$3 = Math.random().toString(8), c$2 = "__d" + f$3, a$2 = "__a" + f$3, s$2 = /(PointerCapture)$|Capture$/i, h$3 = 0, p$3 = V(!1), v$2 = V(!0);
//#endregion
//#region node_modules/preact/hooks/dist/hooks.module.js
var t$2;
var r$1;
var u$2;
var i$2;
var o$1 = 0;
var f$2 = [];
var c$1 = l$3;
var e$1 = c$1.__b;
var a$1 = c$1.__r;
var v$1 = c$1.diffed;
var l$2 = c$1.__c;
var m$1 = c$1.unmount;
var p$2 = c$1.__;
function s$1(n, t) {
	c$1.__h && c$1.__h(r$1, n, o$1 || t), o$1 = 0;
	var u = r$1.__H || (r$1.__H = {
		__: [],
		__h: []
	});
	return n >= u.__.length && u.__.push({}), u.__[n];
}
function d$2(n) {
	return o$1 = 1, y$2(D, n);
}
function y$2(n, u, i) {
	var o = s$1(t$2++, 2);
	if (o.t = n, !o.__c && (o.__ = [i ? i(u) : D(void 0, u), function(n) {
		var t = o.__N ? o.__N[0] : o.__[0], r = o.t(t, n);
		t !== r && (o.__N = [r, o.__[1]], o.__c.setState({}));
	}], o.__c = r$1, !r$1.__f)) {
		var f = function(n, t, r) {
			if (!o.__c.__H) return !0;
			var u = !1, i = o.__c.props !== n;
			if (o.__c.__H.__.some(function(n) {
				if (n.__N) {
					u = !0;
					var t = n.__[0];
					n.__ = n.__N, n.__N = void 0, t !== n.__[0] && (i = !0);
				}
			}), c) {
				var f = c.call(this, n, t, r);
				return u ? f || i : f;
			}
			return !u || i;
		};
		r$1.__f = !0;
		var c = r$1.shouldComponentUpdate, e = r$1.componentWillUpdate;
		r$1.componentWillUpdate = function(n, t, r) {
			if (this.__e) {
				var u = c;
				c = void 0, f(n, t, r), c = u;
			}
			e && e.call(this, n, t, r);
		}, r$1.shouldComponentUpdate = f;
	}
	return o.__N || o.__;
}
function h$2(n, u) {
	var i = s$1(t$2++, 3);
	!c$1.__s && C(i.__H, u) && (i.__ = n, i.u = u, r$1.__H.__h.push(i));
}
function A(n) {
	return o$1 = 5, T(function() {
		return { current: n };
	}, []);
}
function T(n, r) {
	var u = s$1(t$2++, 7);
	return C(u.__H, r) && (u.__ = n(), u.__H = r, u.__h = n), u.__;
}
function q$1(n, t) {
	return o$1 = 8, T(function() {
		return n;
	}, t);
}
function g$2() {
	var n = s$1(t$2++, 11);
	if (!n.__) {
		for (var u = r$1.__v; null !== u && !u.__m && null !== u.__;) u = u.__;
		var i = u.__m || (u.__m = [0, 0]);
		n.__ = "P" + i[0] + "-" + i[1]++;
	}
	return n.__;
}
function j$1() {
	for (var n; n = f$2.shift();) {
		var t = n.__H;
		if (n.__P && t) try {
			t.__h.some(z), t.__h.some(B), t.__h = [];
		} catch (r) {
			t.__h = [], c$1.__e(r, n.__v);
		}
	}
}
c$1.__b = function(n) {
	r$1 = null, e$1 && e$1(n);
}, c$1.__ = function(n, t) {
	n && t.__k && t.__k.__m && (n.__m = t.__k.__m), p$2 && p$2(n, t);
}, c$1.__r = function(n) {
	a$1 && a$1(n), t$2 = 0;
	var i = (r$1 = n.__c).__H;
	i && (u$2 === r$1 ? (i.__h = [], r$1.__h = [], i.__.some(function(n) {
		n.__N && (n.__ = n.__N), n.u = n.__N = void 0;
	})) : (i.__h.some(z), i.__h.some(B), i.__h = [], t$2 = 0)), u$2 = r$1;
}, c$1.diffed = function(n) {
	v$1 && v$1(n);
	var t = n.__c;
	t && t.__H && (t.__H.__h.length && (1 !== f$2.push(t) && i$2 === c$1.requestAnimationFrame || ((i$2 = c$1.requestAnimationFrame) || w$2)(j$1)), t.__H.__.some(function(n) {
		n.u && (n.__H = n.u, n.u = void 0);
	})), u$2 = r$1 = null;
}, c$1.__c = function(n, t) {
	t.some(function(n) {
		try {
			n.__h.some(z), n.__h = n.__h.filter(function(n) {
				return !n.__ || B(n);
			});
		} catch (r) {
			t.some(function(n) {
				n.__h && (n.__h = []);
			}), t = [], c$1.__e(r, n.__v);
		}
	}), l$2 && l$2(n, t);
}, c$1.unmount = function(n) {
	m$1 && m$1(n);
	var t, r = n.__c;
	r && r.__H && (r.__H.__.some(function(n) {
		try {
			z(n);
		} catch (n) {
			t = n;
		}
	}), r.__H = void 0, t && c$1.__e(t, r.__v));
};
var k = "function" == typeof requestAnimationFrame;
function w$2(n) {
	var t, r = function() {
		clearTimeout(u), k && cancelAnimationFrame(t), setTimeout(n);
	}, u = setTimeout(r, 35);
	k && (t = requestAnimationFrame(r));
}
function z(n) {
	var t = r$1, u = n.__c;
	"function" == typeof u && (n.__c = void 0, u()), r$1 = t;
}
function B(n) {
	var t = r$1;
	n.__c = n.__(), r$1 = t;
}
function C(n, t) {
	return !n || n.length !== t.length || t.some(function(t, r) {
		return t !== n[r];
	});
}
function D(n, t) {
	return "function" == typeof t ? t(n) : t;
}
//#endregion
//#region \0vite/preload-helper.js
var scriptRel = "modulepreload";
var assetsURL = function(dep, importerUrl) {
	return new URL(dep, importerUrl).href;
};
var seen = {};
var __vitePreload = function preload(baseModule, deps, importerUrl) {
	let promise = Promise.resolve();
	if (deps && deps.length > 0) {
		const links = document.getElementsByTagName("link");
		const cspNonceMeta = document.querySelector("meta[property=csp-nonce]");
		const cspNonce = cspNonceMeta?.nonce || cspNonceMeta?.getAttribute("nonce");
		function allSettled(promises) {
			return Promise.all(promises.map((p) => Promise.resolve(p).then((value) => ({
				status: "fulfilled",
				value
			}), (reason) => ({
				status: "rejected",
				reason
			}))));
		}
		function importMetaResolve(specifier) {
			if (import.meta.resolve) return import.meta.resolve(specifier);
			return new URL(
				specifier,
				/** #__KEEP__ */
				import.meta.url
			).href;
		}
		promise = allSettled(deps.map((dep) => {
			dep = assetsURL(dep, importerUrl);
			dep = importMetaResolve(dep);
			if (dep in seen) return;
			seen[dep] = true;
			const isCss = dep.endsWith(".css");
			for (let i = links.length - 1; i >= 0; i--) {
				const link = links[i];
				if (link.href === dep && (!isCss || link.rel === "stylesheet")) return;
			}
			const link = document.createElement("link");
			link.rel = isCss ? "stylesheet" : scriptRel;
			if (!isCss) link.as = "script";
			link.crossOrigin = "";
			link.href = dep;
			if (cspNonce) link.setAttribute("nonce", cspNonce);
			document.head.appendChild(link);
			if (isCss) return new Promise((res, rej) => {
				link.addEventListener("load", res);
				link.addEventListener("error", () => rej(/* @__PURE__ */ new Error(`Unable to preload CSS for ${dep}`)));
			});
		}));
	}
	function handlePreloadError(err) {
		const e = new Event("vite:preloadError", { cancelable: true });
		e.payload = err;
		window.dispatchEvent(e);
		if (!e.defaultPrevented) throw err;
	}
	return promise.then((res) => {
		for (const item of res || []) {
			if (item.status !== "rejected") continue;
			handlePreloadError(item.reason);
		}
		return baseModule().catch(handlePreloadError);
	});
};
//#endregion
//#region /@vite-plugin-pwa/virtual:pwa-register
var autoUpdateMode = "false";
var selfDestroying = "false";
var auto = autoUpdateMode === "true";
var autoDestroy = selfDestroying === "true";
function registerSW(options = {}) {
	const { immediate = false, onNeedReload, onNeedRefresh, onOfflineReady, onRegistered, onRegisteredSW, onRegisterError } = options;
	let wb;
	let registerPromise;
	let sendSkipWaitingMessage;
	const updateServiceWorker = async (_reloadPage = true) => {
		await registerPromise;
		if (!auto) sendSkipWaitingMessage?.();
	};
	async function register() {
		if ("serviceWorker" in navigator) {
			wb = await __vitePreload(async () => {
				const { Workbox } = await import("./workbox-window.prod.es5-CkHxznpn.js");
				return { Workbox };
			}, [], import.meta.url).then(({ Workbox }) => {
				return new Workbox("./sw.js", {
					scope: "./",
					type: "classic"
				});
			}).catch((e) => {
				onRegisterError?.(e);
			});
			if (!wb) return;
			sendSkipWaitingMessage = () => {
				wb?.messageSkipWaiting();
			};
			if (!autoDestroy) if (auto) {
				wb.addEventListener("activated", (event) => {
					if (event.isUpdate || event.isExternal) if (onNeedReload) onNeedReload();
					else window.location.reload();
				});
				wb.addEventListener("installed", (event) => {
					if (!event.isUpdate) onOfflineReady?.();
				});
			} else {
				let onNeedRefreshCalled = false;
				const showSkipWaitingPrompt = () => {
					onNeedRefreshCalled = true;
					wb?.addEventListener("controlling", (event) => {
						if (event.isUpdate) if (onNeedReload) onNeedReload();
						else window.location.reload();
					});
					onNeedRefresh?.();
				};
				wb.addEventListener("installed", (event) => {
					if (typeof event.isUpdate === "undefined") if (typeof event.isExternal !== "undefined") if (event.isExternal) showSkipWaitingPrompt();
					else !onNeedRefreshCalled && onOfflineReady?.();
					else !onNeedRefreshCalled && onOfflineReady?.();
					else if (!event.isUpdate) onOfflineReady?.();
				});
				wb.addEventListener("waiting", showSkipWaitingPrompt);
			}
			wb.register({ immediate }).then((r) => {
				if (onRegisteredSW) onRegisteredSW("./sw.js", r);
				else onRegistered?.(r);
			}).catch((e) => {
				onRegisterError?.(e);
			});
		}
	}
	registerPromise = register();
	return updateServiceWorker;
}
//#endregion
//#region src/api.ts
var defaultTimeout = 12e3;
var diagnosticsRefreshTimeout = 55e3;
var APIError = class extends Error {
	status;
	code;
	constructor(status, code, message) {
		super(message);
		this.name = "APIError";
		this.status = status;
		this.code = code;
	}
};
function isEnvelope(value) {
	return Boolean(value && typeof value === "object" && typeof value.ok === "boolean");
}
function unwrapResponse(value) {
	if (!isEnvelope(value)) return value;
	if (value.ok && value.result !== void 0) return value.result;
	const error = value.error;
	throw new APIError(500, error?.code ?? "invalid_response", error?.message ?? "The local API returned an incomplete response.");
}
var RayanLampAPI = class {
	kind = "relay";
	displayName = "Go coordinator";
	csrf = "";
	sessionRequest = null;
	get syncKey() {
		return `relay:${window.location.origin}`;
	}
	get csrfToken() {
		return this.csrf;
	}
	async establishSession(force = false) {
		if (force) this.csrf = "";
		if (this.csrf) return this.csrf;
		if (this.sessionRequest) return this.sessionRequest;
		this.sessionRequest = this.requestSession().finally(() => {
			this.sessionRequest = null;
		});
		return this.sessionRequest;
	}
	async requestSession() {
		const response = await fetch("/app/session", {
			method: "POST",
			credentials: "same-origin",
			headers: { Accept: "application/json" },
			cache: "no-store"
		});
		const result = unwrapResponse(await this.readJSON(response));
		if (!result || typeof result.csrf_token !== "string" || !result.csrf_token) throw new APIError(response.status, "invalid_session", "The local controller did not provide a CSRF token.");
		this.csrf = result.csrf_token;
		return this.csrf;
	}
	async bootstrap() {
		await this.establishSession();
		return this.request("/ui/bootstrap");
	}
	async getConfig() {
		return this.request("/ui/config");
	}
	async patchConfig(patch, revision) {
		const headers = {};
		if (revision !== void 0 && revision !== "") headers["If-Match"] = String(revision);
		return this.request("/ui/config", {
			method: "PATCH",
			headers,
			body: JSON.stringify(patch)
		});
	}
	async beginHotkeyCapture() {
		return this.request("/ui/hotkeys/capture", {
			method: "POST",
			body: JSON.stringify({})
		});
	}
	async renewHotkeyCapture(leaseID) {
		return this.request("/ui/hotkeys/capture", {
			method: "POST",
			body: JSON.stringify({ lease_id: leaseID })
		});
	}
	async releaseHotkeyCapture(leaseID, keepalive = false) {
		return this.request("/ui/hotkeys/capture", {
			method: "DELETE",
			body: JSON.stringify({ lease_id: leaseID }),
			keepalive
		});
	}
	async probeHotkey(leaseID, chord) {
		return this.request("/ui/hotkeys/probe", {
			method: "POST",
			body: JSON.stringify({
				lease_id: leaseID,
				chord
			})
		});
	}
	async integrationRegistrationStatus() {
		return this.request("/ui/integration/registration");
	}
	async updateIntegrationRegistration(method) {
		return this.request("/ui/integration/registration", {
			method: "POST",
			body: JSON.stringify({ method })
		});
	}
	async status() {
		return this.request("/status");
	}
	async command(command) {
		const path = command === "reconnect" ? "/reconnect" : command.startsWith("window/") ? `/${command}` : `/lamp/${command}`;
		return this.request(path, { method: "POST" });
	}
	async diagnostics(endpoint) {
		const query = new URLSearchParams({ endpoint });
		return this.request(`/diagnostics?${query}`);
	}
	async uiDiagnostics() {
		return this.request("/ui/diagnostics");
	}
	async refreshUIDiagnostics(endpoints) {
		return this.request("/ui/diagnostics/refresh", {
			method: "POST",
			body: JSON.stringify(endpoints && endpoints.length > 0 ? { endpoints } : {})
		}, true, diagnosticsRefreshTimeout);
	}
	async notify(title, message) {
		return this.request("/notifications", {
			method: "POST",
			body: JSON.stringify({
				title,
				message
			})
		});
	}
	async playAudio(cue = "message", count = 1) {
		validateAudioRequest(cue, count);
		return this.request("/audio/play", {
			method: "POST",
			body: JSON.stringify({
				cue,
				count
			})
		});
	}
	async importAudio(file, revision) {
		const headers = {
			"Content-Type": "audio/wav",
			"X-RayanLamp-Filename": safeAudioFilenameHeader(file.name)
		};
		if (revision !== void 0 && revision !== "") headers["If-Match"] = String(revision);
		return this.request("/ui/audio/import", {
			method: "POST",
			headers,
			body: file
		});
	}
	realtimeURL() {
		const url = new URL("/events", window.location.href);
		url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
		return url.href;
	}
	realtimeOpened(_socket) {
		return [];
	}
	realtimeClosed() {
		return [];
	}
	decodeRealtimeMessage(raw) {
		if (typeof raw !== "string") return [];
		let parsed;
		try {
			parsed = JSON.parse(raw);
		} catch {
			return [];
		}
		const envelope = parsed && typeof parsed === "object" && "result" in parsed ? parsed.result : parsed;
		return envelope && typeof envelope === "object" ? [envelope] : [];
	}
	async request(path, init = {}, retrySession = true, timeoutMs = defaultTimeout) {
		const method = (init.method ?? "GET").toUpperCase();
		const mutating = method !== "GET" && method !== "HEAD";
		if (mutating) await this.establishSession();
		const controller = new AbortController();
		const timer = window.setTimeout(() => controller.abort(), timeoutMs);
		const headers = new Headers(init.headers);
		headers.set("Accept", "application/json");
		if (init.body !== void 0 && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
		if (mutating) headers.set("X-RayanLamp-CSRF", this.csrf);
		try {
			const response = await fetch(path, {
				...init,
				method,
				headers,
				credentials: "same-origin",
				cache: "no-store",
				signal: controller.signal
			});
			if (response.status === 401 && retrySession && path !== "/app/session") {
				this.csrf = "";
				await this.establishSession(true);
				return this.request(path, init, false, timeoutMs);
			}
			const value = await this.readJSON(response);
			if (!response.ok) {
				const envelope = isEnvelope(value) ? value : void 0;
				throw new APIError(response.status, envelope?.error?.code ?? `http_${response.status}`, envelope?.error?.message ?? `The local controller returned HTTP ${response.status}.`);
			}
			return unwrapResponse(value);
		} catch (error) {
			if (error instanceof DOMException && error.name === "AbortError") throw new APIError(408, "timeout", "The local controller did not respond in time.");
			throw error;
		} finally {
			window.clearTimeout(timer);
		}
	}
	async readJSON(response) {
		if (!(response.headers.get("Content-Type") ?? "").toLowerCase().includes("application/json")) throw new APIError(response.status, "invalid_response", "The local controller returned a non-JSON response.");
		try {
			return await response.json();
		} catch {
			throw new APIError(response.status, "invalid_response", "The local controller returned malformed JSON.");
		}
	}
};
function validateAudioRequest(cue, count) {
	if (![
		"on",
		"off",
		"message",
		"error"
	].includes(cue)) throw new APIError(400, "invalid_audio_cue", "Unknown audio cue.");
	if (!Number.isInteger(count) || count < 1 || count > 10) throw new APIError(400, "invalid_audio_count", "Audio cue count must be an integer between 1 and 10.");
}
function safeAudioFilenameHeader(value) {
	return (value.split(/[\\/]/).at(-1)?.trim() ?? "").replace(/[^\x20-\x7e]/g, "_").replace(/[";]/g, "_").slice(0, 128) || "Custom sound.wav";
}
//#endregion
//#region src/audio.ts
var audioDatabaseName = "rayanlamp.web.audio.v1";
var audioStoreName = "sounds";
var maxAudioBytes = 4 << 20;
var maxAudioSeconds = 10;
var beepIntervalSeconds = .22;
/**
* One browser-side audio pipeline is shared by the direct dashboard, settings
* preview, and future PWA surfaces. Semantic cue names keep callers decoupled
* from physical files, while custom WAVs are de-duplicated by SHA-256 in IDB.
*/
var BrowserAudioPlayer = class {
	fetcher;
	contextFactory;
	baseURL;
	context = null;
	decoded = /* @__PURE__ */ new Map();
	playbackTail = Promise.resolve();
	nextStartTime = 0;
	unlockWaiters = /* @__PURE__ */ new Set();
	constructor(options = {}) {
		this.fetcher = options.fetch ?? ((input, init) => globalThis.fetch(input, init));
		this.contextFactory = options.contextFactory ?? createBrowserAudioContext;
		this.baseURL = options.baseURL ?? (() => document.baseURI);
	}
	play(cue, settings, count = 1) {
		const normalizedCount = normalizeCueCount(count);
		const context = this.audioContext();
		const resumed = this.ensureRunning(context);
		const operation = this.playbackTail.catch(() => void 0).then(async () => {
			await resumed;
			const { key, decoded } = await this.resolveDecodedAsset(context, cue, settings);
			let buffer;
			try {
				buffer = await decoded;
			} catch (error) {
				this.decoded.delete(key);
				throw error;
			}
			const spacing = Math.max(beepIntervalSeconds, buffer.duration + .03);
			const firstStart = Math.max(context.currentTime, this.nextStartTime);
			for (let index = 0; index < normalizedCount; index++) {
				const source = context.createBufferSource();
				source.buffer = buffer;
				source.connect(context.destination);
				source.start(firstStart + index * spacing);
			}
			this.nextStartTime = firstStart + normalizedCount * spacing;
		});
		this.playbackTail = operation;
		return operation;
	}
	async importCustomWAV(file) {
		if (file.size > maxAudioBytes) throw new Error("Custom audio must be 4 MiB or smaller.");
		const data = await file.arrayBuffer();
		const info = inspectWAV(data);
		const id = await audioContentID(data);
		const importedAt = (/* @__PURE__ */ new Date()).toISOString();
		const record = {
			id,
			name: safeDisplayName(file.name),
			bytes: data.byteLength,
			importedAt,
			...info,
			data
		};
		await putStoredAudio(record);
		this.decoded.delete(`custom:${id}`);
		return withoutData(record);
	}
	async describe(id) {
		if (!validAssetID(id)) return null;
		try {
			const record = await getStoredAudio(id);
			if (!record || await audioContentID(record.data) !== id) return null;
			inspectWAV(record.data);
			return withoutData(record);
		} catch {
			return null;
		}
	}
	armUserGestureUnlock(target = document) {
		const unlock = () => {
			let context;
			try {
				context = this.audioContext();
			} catch {
				return;
			}
			if (context.state === "running") {
				for (const resolve of this.unlockWaiters) resolve();
				this.unlockWaiters.clear();
				return;
			}
			context.resume().then(() => {
				if (context.state !== "running") return;
				for (const resolve of this.unlockWaiters) resolve();
				this.unlockWaiters.clear();
			}).catch(() => void 0);
		};
		target.addEventListener("pointerdown", unlock, {
			capture: true,
			passive: true
		});
		target.addEventListener("keydown", unlock, { capture: true });
		return () => {
			target.removeEventListener("pointerdown", unlock, true);
			target.removeEventListener("keydown", unlock, true);
		};
	}
	audioContext() {
		if (!this.context || this.context.state === "closed") this.context = this.contextFactory();
		return this.context;
	}
	async ensureRunning(context) {
		if (audioContextRunning(context)) return;
		try {
			await context.resume();
		} catch {}
		if (audioContextRunning(context)) return;
		await new Promise((resolve) => {
			this.unlockWaiters.add(resolve);
		});
	}
	async resolveDecodedAsset(context, cue, settings) {
		if (settings.sound_source === "custom" && validAssetID(settings.sound_file)) {
			const customKey = `custom:${settings.sound_file}`;
			const cached = this.decoded.get(customKey);
			if (cached) return {
				key: customKey,
				decoded: cached
			};
			try {
				const record = await getStoredAudio(settings.sound_file);
				if (record && await audioContentID(record.data) === settings.sound_file) {
					inspectWAV(record.data);
					const decoded = context.decodeAudioData(record.data.slice(0));
					this.decoded.set(customKey, decoded);
					return {
						key: customKey,
						decoded
					};
				}
			} catch {}
		}
		return this.resolveBundledAsset(context, cue);
	}
	resolveBundledAsset(context, cue) {
		const key = `bundled:${cue}`;
		const cached = this.decoded.get(key);
		if (cached) return {
			key,
			decoded: cached
		};
		const decoded = (async () => {
			const response = await this.fetcher(bundledAudioURL(cue, this.baseURL()), {
				credentials: "same-origin",
				cache: "default"
			});
			if (!response.ok) throw new Error(`Bundled ${cue} sound is unavailable (HTTP ${response.status}).`);
			const data = await response.arrayBuffer();
			inspectWAV(data);
			return context.decodeAudioData(data.slice(0));
		})();
		this.decoded.set(key, decoded);
		return {
			key,
			decoded
		};
	}
};
var browserAudio = new BrowserAudioPlayer();
function bundledAudioURL(cue, baseURL) {
	return new URL(`shared/sfx/${cue}.wav`, baseURL).href;
}
function inspectWAV(data) {
	if (data.byteLength < 44) throw new Error("Custom audio is too small to be a WAV file.");
	if (data.byteLength > maxAudioBytes) throw new Error("Custom audio must be 4 MiB or smaller.");
	const bytes = new Uint8Array(data);
	const view = new DataView(data);
	if (fourCC(bytes, 0) !== "RIFF" || fourCC(bytes, 8) !== "WAVE") throw new Error("Custom audio must use the RIFF/WAVE container.");
	if (view.getUint32(4, true) + 8 !== data.byteLength) throw new Error("Custom audio has an inconsistent RIFF size.");
	let formatOffset = -1;
	let formatBytes = 0;
	let dataBytes = 0;
	let dataSeen = false;
	for (let offset = 12; offset < bytes.length;) {
		if (offset + 8 > bytes.length) throw new Error("Custom audio contains a truncated WAV chunk header.");
		const kind = fourCC(bytes, offset);
		const size = view.getUint32(offset + 4, true);
		const body = offset + 8;
		const end = body + size;
		const paddedEnd = end + (size & 1);
		if (end > bytes.length || paddedEnd > bytes.length) throw new Error("Custom audio contains a truncated WAV chunk.");
		if (kind === "fmt ") {
			if (formatOffset >= 0) throw new Error("Custom audio contains duplicate fmt chunks.");
			formatOffset = body;
			formatBytes = size;
		} else if (kind === "data") {
			if (dataSeen) throw new Error("Custom audio contains duplicate data chunks.");
			dataSeen = true;
			dataBytes = size;
		}
		offset = paddedEnd;
	}
	if (formatOffset < 0 || formatBytes < 16 || dataBytes < 1) throw new Error("Custom audio needs valid fmt and data chunks.");
	if (view.getUint16(formatOffset, true) !== 1) throw new Error("Custom audio must be uncompressed PCM WAV.");
	const channels = view.getUint16(formatOffset + 2, true);
	const sampleRate = view.getUint32(formatOffset + 4, true);
	const byteRate = view.getUint32(formatOffset + 8, true);
	const blockAlign = view.getUint16(formatOffset + 12, true);
	const bitsPerSample = view.getUint16(formatOffset + 14, true);
	if (channels < 1 || channels > 2) throw new Error("Custom audio must be mono or stereo.");
	if (sampleRate < 8e3 || sampleRate > 96e3) throw new Error("Custom audio sample rate must be between 8 kHz and 96 kHz.");
	if (![
		8,
		16,
		24,
		32
	].includes(bitsPerSample)) throw new Error("Custom audio must use 8, 16, 24, or 32-bit PCM.");
	const expectedAlign = channels * bitsPerSample / 8;
	if (blockAlign !== expectedAlign || byteRate !== sampleRate * expectedAlign) throw new Error("Custom audio has inconsistent PCM format metadata.");
	if (dataBytes % blockAlign !== 0) throw new Error("Custom audio PCM data is not block-aligned.");
	const frames = dataBytes / blockAlign;
	if (frames > sampleRate * maxAudioSeconds) throw new Error(`Custom audio must be no longer than ${maxAudioSeconds} seconds.`);
	const durationMs = Math.max(1, Math.round(frames / sampleRate * 1e3));
	return {
		channels,
		sampleRate,
		bitsPerSample,
		dataBytes,
		durationMs
	};
}
function createBrowserAudioContext() {
	const AudioContextConstructor = globalThis.AudioContext ?? globalThis.webkitAudioContext;
	if (!AudioContextConstructor) throw new Error("This browser does not support Web Audio playback.");
	return new AudioContextConstructor();
}
function audioContextRunning(context) {
	return context.state === "running";
}
function normalizeCueCount(value) {
	if (!Number.isInteger(value) || value < 1) throw new Error("Audio cue count must be a positive integer.");
	return Math.min(value, 10);
}
function fourCC(bytes, offset) {
	return String.fromCharCode(bytes[offset], bytes[offset + 1], bytes[offset + 2], bytes[offset + 3]);
}
function safeDisplayName(value) {
	const leaf = value.split(/[\\/]/).at(-1)?.trim() ?? "";
	return Array.from(leaf).filter((character) => {
		const code = character.codePointAt(0) ?? 0;
		return code >= 32 && code !== 127;
	}).join("").slice(0, 128) || "Custom sound.wav";
}
function validAssetID(value) {
	return /^[a-f0-9]{64}$/.test(value);
}
async function audioContentID(data) {
	if (globalThis.crypto?.subtle) {
		const digest = await globalThis.crypto.subtle.digest("SHA-256", data);
		return hex(new Uint8Array(digest));
	}
	return sha256Hex(new Uint8Array(data));
}
function sha256Hex(input) {
	const constants = [
		1116352408,
		1899447441,
		3049323471,
		3921009573,
		961987163,
		1508970993,
		2453635748,
		2870763221,
		3624381080,
		310598401,
		607225278,
		1426881987,
		1925078388,
		2162078206,
		2614888103,
		3248222580,
		3835390401,
		4022224774,
		264347078,
		604807628,
		770255983,
		1249150122,
		1555081692,
		1996064986,
		2554220882,
		2821834349,
		2952996808,
		3210313671,
		3336571891,
		3584528711,
		113926993,
		338241895,
		666307205,
		773529912,
		1294757372,
		1396182291,
		1695183700,
		1986661051,
		2177026350,
		2456956037,
		2730485921,
		2820302411,
		3259730800,
		3345764771,
		3516065817,
		3600352804,
		4094571909,
		275423344,
		430227734,
		506948616,
		659060556,
		883997877,
		958139571,
		1322822218,
		1537002063,
		1747873779,
		1955562222,
		2024104815,
		2227730452,
		2361852424,
		2428436474,
		2756734187,
		3204031479,
		3329325298
	];
	const paddedLength = Math.ceil((input.length + 9) / 64) * 64;
	const message = new Uint8Array(paddedLength);
	message.set(input);
	message[input.length] = 128;
	const view = new DataView(message.buffer);
	const bitLength = input.length * 8;
	view.setUint32(paddedLength - 8, Math.floor(bitLength / 4294967296));
	view.setUint32(paddedLength - 4, bitLength >>> 0);
	const hash = new Uint32Array([
		1779033703,
		3144134277,
		1013904242,
		2773480762,
		1359893119,
		2600822924,
		528734635,
		1541459225
	]);
	const words = /* @__PURE__ */ new Uint32Array(64);
	for (let offset = 0; offset < message.length; offset += 64) {
		for (let index = 0; index < 16; index++) words[index] = view.getUint32(offset + index * 4);
		for (let index = 16; index < 64; index++) {
			const before15 = words[index - 15];
			const before2 = words[index - 2];
			const sigma0 = rotateRight(before15, 7) ^ rotateRight(before15, 18) ^ before15 >>> 3;
			const sigma1 = rotateRight(before2, 17) ^ rotateRight(before2, 19) ^ before2 >>> 10;
			words[index] = words[index - 16] + sigma0 + words[index - 7] + sigma1 >>> 0;
		}
		let [a, b, c, d, e, f, g, h] = hash;
		for (let index = 0; index < 64; index++) {
			const sum1 = rotateRight(e, 6) ^ rotateRight(e, 11) ^ rotateRight(e, 25);
			const choice = e & f ^ ~e & g;
			const temp1 = h + sum1 + choice + constants[index] + words[index] >>> 0;
			const temp2 = (rotateRight(a, 2) ^ rotateRight(a, 13) ^ rotateRight(a, 22)) + (a & b ^ a & c ^ b & c) >>> 0;
			h = g;
			g = f;
			f = e;
			e = d + temp1 >>> 0;
			d = c;
			c = b;
			b = a;
			a = temp1 + temp2 >>> 0;
		}
		hash[0] = hash[0] + a >>> 0;
		hash[1] = hash[1] + b >>> 0;
		hash[2] = hash[2] + c >>> 0;
		hash[3] = hash[3] + d >>> 0;
		hash[4] = hash[4] + e >>> 0;
		hash[5] = hash[5] + f >>> 0;
		hash[6] = hash[6] + g >>> 0;
		hash[7] = hash[7] + h >>> 0;
	}
	const output = /* @__PURE__ */ new Uint8Array(32);
	const outputView = new DataView(output.buffer);
	for (let index = 0; index < hash.length; index++) outputView.setUint32(index * 4, hash[index]);
	return hex(output);
}
function rotateRight(value, amount) {
	return value >>> amount | value << 32 - amount;
}
function hex(bytes) {
	return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}
function openAudioDatabase() {
	if (typeof indexedDB === "undefined") return Promise.reject(/* @__PURE__ */ new Error("This browser cannot store a custom sound."));
	return new Promise((resolve, reject) => {
		const request = indexedDB.open(audioDatabaseName, 1);
		request.onupgradeneeded = () => {
			if (!request.result.objectStoreNames.contains(audioStoreName)) request.result.createObjectStore(audioStoreName, { keyPath: "id" });
		};
		request.onsuccess = () => resolve(request.result);
		request.onerror = () => reject(request.error ?? /* @__PURE__ */ new Error("The custom audio database could not be opened."));
	});
}
async function getStoredAudio(id) {
	const database = await openAudioDatabase();
	try {
		const request = database.transaction(audioStoreName, "readonly").objectStore(audioStoreName).get(id);
		return await new Promise((resolve, reject) => {
			request.onsuccess = () => resolve(request.result ?? null);
			request.onerror = () => reject(request.error ?? /* @__PURE__ */ new Error("The custom sound could not be read."));
		});
	} finally {
		database.close();
	}
}
async function putStoredAudio(record) {
	const database = await openAudioDatabase();
	try {
		const transaction = database.transaction(audioStoreName, "readwrite");
		transaction.objectStore(audioStoreName).put(record);
		await new Promise((resolve, reject) => {
			transaction.oncomplete = () => resolve();
			transaction.onabort = () => reject(transaction.error ?? /* @__PURE__ */ new Error("The custom sound import was cancelled."));
			transaction.onerror = () => reject(transaction.error ?? /* @__PURE__ */ new Error("The custom sound could not be stored."));
		});
	} finally {
		database.close();
	}
}
function withoutData(record) {
	const { data: _data, ...metadata } = record;
	return metadata;
}
//#endregion
//#region src/build-edition.ts
var buildEdition = "develop";
//#endregion
//#region src/direct-protocol.ts
var DIRECT_DIAGNOSTIC_ENDPOINTS = [
	"/info",
	"/update/info",
	"/wifi/signal",
	"/wifi/status",
	"/wifi/country",
	"/description.xml"
];
var DIRECT_PROJECTION_DIAGNOSTIC_ENDPOINTS = ["/config/ap", "/config/st"];
var stateAliases = {
	ON: "on",
	"1": "on",
	TRUE: "on",
	HIGH: "on",
	OFF: "off",
	"0": "off",
	FALSE: "off",
	LOW: "off"
};
var stateObjectKeys = /* @__PURE__ */ new Set([
	"state",
	"status",
	"lamp",
	"value"
]);
var auditedEndpointSet = new Set(DIRECT_DIAGNOSTIC_ENDPOINTS);
var projectionEndpointSet = new Set(DIRECT_PROJECTION_DIAGNOSTIC_ENDPOINTS);
var usefulDescriptionTags = /* @__PURE__ */ new Set([
	"friendly_name",
	"model_name",
	"model_number",
	"manufacturer",
	"manufacturer_url",
	"serial_number",
	"udn",
	"device_type",
	"presentation_url"
]);
var sensitiveMarkers = [
	"password",
	"passwd",
	"passphrase",
	"secret",
	"token",
	"authorization",
	"credential",
	"cookie",
	"session",
	"wepkey"
];
var maxRemoteEventBytes = 16384;
var maxRemoteMessageBytes = 4096;
var maxDiagnosticBytes = 1 << 20;
var maxDiagnosticFields = 256;
var maxDiagnosticDepth = 12;
var redactedValue = "[redacted]";
function normalizeDirectLampURL(raw) {
	let value = raw.trim();
	if (!value) throw new TypeError("Lamp URL is required.");
	if (/[\u0000-\u001f\u007f\\]/u.test(value)) throw new TypeError("Lamp URL contains invalid characters.");
	if (!value.includes("://")) value = `http://${value}`;
	if (value.includes("?") || value.includes("#")) throw new TypeError("Lamp URL must not contain a query or fragment.");
	let parsed;
	try {
		parsed = new URL(value);
	} catch {
		throw new TypeError("Lamp URL is invalid.");
	}
	if (parsed.protocol !== "http:" && parsed.protocol !== "https:") throw new TypeError("Lamp URL must use HTTP or HTTPS.");
	if (!parsed.hostname) throw new TypeError("Lamp URL must include a hostname.");
	if (parsed.username || parsed.password) throw new TypeError("Lamp URL must not contain credentials.");
	if (parsed.pathname !== "/") throw new TypeError("Lamp URL must not contain a path prefix.");
	return parsed.origin;
}
function deriveDirectWebSocketURL(rawBaseURL) {
	const parsed = new URL(normalizeDirectLampURL(rawBaseURL));
	parsed.protocol = parsed.protocol === "https:" ? "wss:" : "ws:";
	parsed.pathname = "/ws";
	return parsed.href;
}
function directLampCommandURL(rawBaseURL, command) {
	return `${normalizeDirectLampURL(rawBaseURL)}/lamp/${command}`;
}
function isDirectDiagnosticEndpoint(endpoint) {
	return auditedEndpointSet.has(endpoint);
}
function directDiagnosticURL(rawBaseURL, endpoint) {
	requireAuditedEndpoint(endpoint);
	return `${normalizeDirectLampURL(rawBaseURL)}${endpoint}`;
}
function directProjectionDiagnosticURLs(rawBaseURL) {
	const baseURL = normalizeDirectLampURL(rawBaseURL);
	return DIRECT_PROJECTION_DIAGNOSTIC_ENDPOINTS.map((endpoint) => ({
		endpoint,
		url: `${baseURL}${endpoint}`
	}));
}
function parseDirectLampState(payload) {
	const text = stripLeadingBOM(payload).trim();
	const direct = parseStateScalar(text);
	if (direct !== "unknown") return direct;
	let decoded;
	try {
		decoded = JSON.parse(text);
	} catch {
		return "unknown";
	}
	if (typeof decoded === "string") return parseStateScalar(decoded);
	if (!isPlainRecord(decoded)) return "unknown";
	for (const [key, value] of Object.entries(decoded)) {
		if (!stateObjectKeys.has(key.toLowerCase())) continue;
		if (typeof value === "string") {
			const state = parseStateScalar(value);
			if (state !== "unknown") return state;
		} else if (typeof value === "boolean") return value ? "on" : "off";
	}
	return "unknown";
}
function parseDirectWebSocketFrame(frame) {
	if (typeof frame !== "string" || frame.length === 0 || utf8ByteLength(frame) > maxRemoteEventBytes || frame.includes("\0")) return null;
	const text = frame.trim();
	if (!text) return null;
	if (/^(?:ON|OFF)$/iu.test(text)) return {
		type: "snapshot",
		state: text.toUpperCase() === "ON" ? "on" : "off",
		raw: text
	};
	if (hasPrefixFold(text, "message:")) {
		const message = text.slice(8).trim();
		if (!validRemoteMessage(message)) return null;
		return {
			type: "message",
			title: "Rayan Lamp",
			message,
			icon: 0,
			raw: text
		};
	}
	if (hasPrefixFold(text, "msg:")) {
		const fields = text.slice(4);
		const separator = fields.indexOf(",");
		if (separator < 0) return null;
		const iconText = fields.slice(0, separator).trim();
		if (!/^[+-]?\d+$/u.test(iconText)) return null;
		const icon = Number(iconText);
		if (!Number.isSafeInteger(icon) || icon < 0 || icon > 255) return null;
		let message;
		try {
			message = decodeURIComponent(fields.slice(separator + 1).replaceAll("+", " ")).trim();
		} catch {
			return null;
		}
		if (!validRemoteMessage(message)) return null;
		return {
			type: "message",
			title: "Rayan Lamp",
			message,
			icon,
			raw: text
		};
	}
	if (hasPrefixFold(text, "beep:")) {
		const countText = text.slice(5).trim();
		if (!/^[+-]?\d+$/u.test(countText)) return null;
		const count = Number(countText);
		if (!Number.isSafeInteger(count) || count < 1 || count > 10) return null;
		return {
			type: "beep",
			count,
			raw: text
		};
	}
	return null;
}
function parseDurationMilliseconds(value) {
	const input = value.trim();
	if (!input) return null;
	if (input === "0") return 0;
	const token = /(\d+(?:\.\d+)?)(ms|s|m|h)/giu;
	let cursor = 0;
	let total = 0;
	let matched = false;
	for (const match of input.matchAll(token)) {
		if (match.index !== cursor) return null;
		cursor += match[0].length;
		matched = true;
		const amount = Number(match[1]);
		const multiplier = match[2].toLowerCase() === "ms" ? 1 : match[2].toLowerCase() === "s" ? 1e3 : match[2].toLowerCase() === "m" ? 6e4 : 36e5;
		total += amount * multiplier;
		if (!Number.isFinite(total) || total > Number.MAX_SAFE_INTEGER) return null;
	}
	return matched && cursor === input.length ? total : null;
}
function parseDirectDiagnostic(endpoint, payload, fetchedAt = /* @__PURE__ */ new Date()) {
	requireAuditedEndpoint(endpoint);
	return parseDiagnosticDocumentCore(endpoint, payload, fetchedAt);
}
function parseDirectProjectionDiagnostic(endpoint, payload, fetchedAt = /* @__PURE__ */ new Date()) {
	requireProjectionEndpoint(endpoint);
	return parseDiagnosticDocumentCore(endpoint, payload, fetchedAt);
}
function parseDiagnosticDocumentCore(endpoint, payload, fetchedAt) {
	const bytes = utf8ByteLength(payload);
	if (bytes > maxDiagnosticBytes) throw new RangeError("Diagnostic response exceeds 1 MiB.");
	const warnings = [];
	if (payload.includes("\0")) warnings.push("response contained NUL bytes");
	const raw = stripLeadingBOM(payload).replace(/\0+$/gu, "");
	const input = raw.trim();
	let format = "text";
	let fields = [];
	let safeRaw = redactText$1(raw);
	if (!input) warnings.push("endpoint returned an empty response");
	else if (input.startsWith("{") || input.startsWith("[")) try {
		const decoded = JSON.parse(input);
		format = "json";
		fields = flattenDiagnosticJSON(endpoint, decoded, warnings);
		safeRaw = JSON.stringify(redactJSONValue("", decoded), null, 2);
	} catch (error) {
		warnings.push(`JSON-looking response was malformed: ${errorMessage$2(error)}`);
		fields = parseTextDiagnosticFields(endpoint, input, warnings);
	}
	else if (input.startsWith("<") || endpoint === "/description.xml") {
		const parsed = parseDescriptionXML(endpoint, input);
		if (parsed.ok) {
			format = "xml";
			fields = parsed.fields;
			safeRaw = redactXML(raw);
		} else {
			warnings.push(`XML-looking response was malformed: ${parsed.error}`);
			fields = parseTextDiagnosticFields(endpoint, input, warnings);
		}
	} else fields = parseTextDiagnosticFields(endpoint, input, warnings);
	fields = makeFieldKeysUnique(fields);
	return {
		endpoint,
		format,
		fetched_at: validDate(fetchedAt).toISOString(),
		bytes,
		raw: safeRaw.replaceAll("\0", "␀"),
		fields,
		...warnings.length > 0 ? { warnings } : {}
	};
}
function projectDirectDiagnosticSummary(documents) {
	const info = {};
	const device = documents["/info"];
	if (device) projectInfo(info, device);
	const signal = documents["/wifi/signal"];
	if (signal) projectSignal(info, signal);
	const wifi = documents["/wifi/status"];
	if (wifi) projectWiFi(info, wifi);
	const station = documents["/config/st"];
	if (station) projectStationConfig(info, station);
	const accessPoint = documents["/config/ap"];
	if (accessPoint) projectAccessPointConfig(info, accessPoint);
	const country = documents["/wifi/country"];
	if (country) info.country = fieldText(country, "country", "country_code", "code", "value", "line_1").toUpperCase();
	const update = documents["/update/info"];
	if (update) projectUpdate(info, update);
	const description = documents["/description.xml"];
	if (description) projectDescription(info, description);
	if (!info.device_name) info.device_name = info.description?.friendly_name;
	return info;
}
var normalizeLampBaseURL = normalizeDirectLampURL;
var directWebSocketURL = deriveDirectWebSocketURL;
var parseLampStatePayload = parseDirectLampState;
var parseDirectEvent = parseDirectWebSocketFrame;
var parseDiagnosticDocument = parseDirectDiagnostic;
var diagnosticDeviceFromDocuments = projectDirectDiagnosticSummary;
function parseStateScalar(value) {
	return stateAliases[value.trim().toUpperCase()] ?? "unknown";
}
function hasPrefixFold(value, prefix) {
	return value.length >= prefix.length && value.slice(0, prefix.length).toLowerCase() === prefix.toLowerCase();
}
function validRemoteMessage(value) {
	return value.length > 0 && utf8ByteLength(value) <= maxRemoteMessageBytes && !value.includes("\0") && !/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/u.test(value);
}
function requireAuditedEndpoint(endpoint) {
	if (!isDirectDiagnosticEndpoint(endpoint)) throw new TypeError("Diagnostic endpoint is not in the audited allowlist.");
}
function requireProjectionEndpoint(endpoint) {
	if (!projectionEndpointSet.has(endpoint)) throw new TypeError("Diagnostic endpoint is not a private projection source.");
}
function flattenDiagnosticJSON(endpoint, value, warnings) {
	const fields = [];
	const visit = (current, prefix, depth) => {
		if (fields.length >= maxDiagnosticFields) return;
		if (depth > maxDiagnosticDepth) {
			if (!warnings.includes("diagnostic nesting was truncated")) warnings.push("diagnostic nesting was truncated");
			return;
		}
		if (Array.isArray(current)) {
			if (current.length === 0) {
				fields.push(classifyField(endpoint, prefix || "value", "[]"));
				return;
			}
			for (let index = 0; index < current.length; index += 1) visit(current[index], `${prefix || "value"}[${index}]`, depth + 1);
			return;
		}
		if (isPlainRecord(current)) {
			for (const key of Object.keys(current).sort()) visit(current[key], prefix ? `${prefix}.${key}` : key, depth + 1);
			return;
		}
		const field = classifyField(endpoint, prefix || "value", jsonScalar(current));
		if (typeof current === "boolean" && field.kind === "string") {
			field.kind = "boolean";
			field.boolean = current;
		} else if (typeof current === "number" && Number.isFinite(current) && field.kind === "string") if (Number.isSafeInteger(current)) {
			field.kind = "integer";
			field.integer = current;
		} else {
			field.kind = "number";
			field.number = current;
		}
		fields.push(field);
	};
	visit(value, "", 0);
	if (fields.length >= maxDiagnosticFields) warnings.push("diagnostic fields were truncated");
	return fields.slice(0, maxDiagnosticFields);
}
function parseTextDiagnosticFields(endpoint, input, warnings) {
	const fields = [];
	let unkeyed = 0;
	for (const source of input.replace(/\r\n?/gu, "\n").split("\n")) {
		const line = source.trim();
		if (!line) continue;
		if (line.toLowerCase() === "fail") warnings.push("firmware reported failure");
		const pair = splitKeyValueLine(line);
		if (pair) fields.push(classifyField(endpoint, pair.key, pair.value));
		else {
			unkeyed += 1;
			let key = `line_${unkeyed}`;
			if (endpoint === "/wifi/signal") {
				if (unkeyed === 1) key = "rssi";
				if (unkeyed === 2) key = "quality";
			} else if (endpoint === "/wifi/country" && unkeyed === 1) key = "country";
			fields.push(classifyField(endpoint, key, line));
		}
		if (fields.length >= maxDiagnosticFields) {
			warnings.push("diagnostic fields were truncated");
			break;
		}
	}
	return fields;
}
function parseDescriptionXML(endpoint, input) {
	if (/<!DOCTYPE|<!ENTITY/iu.test(input)) return {
		ok: false,
		error: "DOCTYPE and entity declarations are not allowed"
	};
	const document = new DOMParser().parseFromString(input, "application/xml");
	if (document.querySelector("parsererror")) return {
		ok: false,
		error: "invalid XML"
	};
	const fields = [];
	for (const element of Array.from(document.querySelectorAll("*"))) {
		const key = canonicalKey(element.localName);
		if (!usefulDescriptionTags.has(key) || element.children.length > 0 || fields.length >= maxDiagnosticFields) continue;
		const value = element.textContent?.trim() ?? "";
		if (value) fields.push(classifyField(endpoint, element.localName, value));
	}
	if (fields.length === 0) return {
		ok: false,
		error: "no recognized device description fields"
	};
	return {
		ok: true,
		fields
	};
}
function classifyField(endpoint, sourceKey, rawValue) {
	const key = canonicalKey(sourceKey) || "value";
	const value = rawValue.trim();
	const field = {
		key,
		label: humanizeKey(sourceKey || key),
		value,
		kind: "string"
	};
	if (isSensitiveKey(key)) {
		field.value = redactedValue;
		field.kind = "redacted";
		field.redacted = true;
		return field;
	}
	const lower = value.toLowerCase();
	if (isSignalKey(key)) {
		const number = strictInteger(lower.replace(/dbm$/iu, "").trim());
		if (number !== null) {
			field.kind = "signal";
			field.unit = "dBm";
			field.integer = number;
		}
	} else if (isPercentKey(key)) {
		const number = strictNumber(value.replace(/%$/u, "").trim());
		if (number !== null) {
			field.kind = "percent";
			field.unit = "%";
			field.number = number;
		}
	} else if (isByteKey(endpoint, key)) {
		const number = parseByteCount(value);
		if (number !== null) {
			field.kind = "bytes";
			field.unit = "bytes";
			field.integer = number;
		}
	} else if (key === "uptime" || key === "uptime_seconds") {
		const seconds = key === "uptime_seconds" ? strictInteger(value) : parseUptime(value);
		if (seconds !== null) {
			field.kind = "duration";
			field.unit = "seconds";
			field.duration_seconds = seconds;
		}
	} else if (isBooleanKey(key)) {
		const boolean = parseBoolean(value);
		if (boolean !== null) {
			field.kind = "boolean";
			field.boolean = boolean;
		}
	} else if (isTimeKey(key)) {
		const time = parseWireTime(value);
		if (time) {
			field.kind = "time";
			field.time = time;
		}
	} else if (isMACKey(key) && /^(?:[0-9a-f]{2}[:-]){5}[0-9a-f]{2}$/iu.test(value)) field.kind = "mac";
	else if (isIPKey(key) && isIPAddress(value)) field.kind = "ip";
	else if (isHashKey(key)) field.kind = "hash";
	else if (key === "state") field.kind = "state";
	else if (isIntegerKey(key)) {
		const integer = strictInteger(value);
		if (integer !== null) {
			field.kind = "integer";
			field.integer = integer;
		}
	}
	return field;
}
function makeFieldKeysUnique(fields) {
	const seen = /* @__PURE__ */ new Map();
	return fields.map((field, index) => {
		const original = field.key || `field_${index + 1}`;
		const count = (seen.get(original) ?? 0) + 1;
		seen.set(original, count);
		return count === 1 ? field : {
			...field,
			key: `${original}_${count}`
		};
	});
}
function splitKeyValueLine(line) {
	const candidates = [line.indexOf(":"), line.indexOf("=")].filter((value) => value >= 0);
	if (candidates.length === 0) return null;
	const separator = Math.min(...candidates);
	const key = line.slice(0, separator).trim();
	if (!key || utf8ByteLength(key) > 80 || !/^\p{L}[\p{L}\p{N}\s._/()\-]*$/u.test(key)) return null;
	return {
		key,
		value: line.slice(separator + 1).trim()
	};
}
function canonicalKey(input) {
	return input.trim().replace(/([\p{Ll}\p{N}])(\p{Lu})/gu, "$1_$2").replace(/[^\p{L}\p{N}]+/gu, "_").replace(/^_+|_+$/gu, "").toLowerCase();
}
function humanizeKey(input) {
	const acronyms = /* @__PURE__ */ new Set([
		"AP",
		"ESP",
		"ID",
		"IP",
		"MAC",
		"MD5",
		"RSSI",
		"SSID",
		"UDN",
		"URL",
		"WIFI"
	]);
	return input.trim().replaceAll(".", " · ").replace(/[_-]+/gu, " ").split(/\s+/u).filter(Boolean).map((word) => {
		const upper = word.toUpperCase();
		return acronyms.has(upper) ? upper : `${word.slice(0, 1).toUpperCase()}${word.slice(1)}`;
	}).join(" ");
}
function isSensitiveKey(key) {
	const compact = key.replaceAll("_", "");
	return sensitiveMarkers.some((marker) => compact.includes(marker)) || [
		"pass",
		"pwd",
		"psk"
	].some((short) => key === short || key.endsWith(`_${short}`));
}
function redactText$1(input) {
	return input.replace(/\r\n?/gu, "\n").replace(/("(?:[^"\\]|\\.)*(?:password|passwd|passphrase|secret|token|authorization|credential|cookie|session)(?:[^"\\]|\\.)*"\s*:\s*)("(?:[^"\\]|\\.)*"|[^,\s}\]]+)/giu, `$1"${redactedValue}"`).replace(/("(?:pass|pwd|psk|wep[_-]?key|(?:[^"\\]|\\.)*[_-](?:pass|pwd|psk|wep[_-]?key))"\s*:\s*)("(?:[^"\\]|\\.)*"|[^,\s}\]]+)/giu, `$1"${redactedValue}"`).split("\n").map((line) => {
		const pair = splitKeyValueLine(line.trim());
		if (!pair || !isSensitiveKey(canonicalKey(pair.key))) return line;
		const separator = firstSeparatorIndex(line);
		return separator < 0 ? line : `${line.slice(0, separator + 1)} ${redactedValue}`;
	}).join("\n");
}
function redactXML(input) {
	return redactText$1(input).replace(new RegExp(`(<([\\w:.-]*(?:password|passwd|passphrase|secret|token|authorization|credential|cookie|session)[\\w:.-]*)\\b[^>]*>)[\\s\\S]*?(<\\/\\2\\s*>)`, "giu"), `$1${redactedValue}$3`);
}
function redactJSONValue(key, value) {
	if (isSensitiveKey(canonicalKey(key))) return redactedValue;
	if (Array.isArray(value)) return value.map((child) => redactJSONValue(key, child));
	if (isPlainRecord(value)) return Object.fromEntries(Object.entries(value).map(([childKey, child]) => [childKey, redactJSONValue(childKey, child)]));
	return value;
}
function projectInfo(info, document) {
	info.device_name = fieldText(document, "device", "device_name", "name");
	info.hostname = fieldText(document, "hostname", "host_name");
	info.firmware = fieldText(document, "firmware", "firmware_version", "version");
	info.wifi_version = fieldText(document, "wifi_version");
	info.esp_chip_id = fieldText(document, "esp_chip_id", "chip_id");
	info.flash_chip_id = fieldText(document, "flash_chip_id");
	const firmwareHash = fieldText(document, "firmware_hash", "hash");
	if (firmwareHash) {
		info.firmware_corrupted = firmwareHash.toUpperCase().includes("CORRUPTED");
		info.firmware_hash = firmwareHash.split("(", 1)[0].trim();
	}
	const protocol = fieldText(document, "protocol_rev", "protocol_revision");
	if (protocol) {
		info.protocol_match = !protocol.toLowerCase().includes("no match");
		info.protocol_revision = protocol.split("(", 1)[0].trim();
	}
	info.station_mac = fieldText(document, "st_mac", "station_mac");
	info.access_point_mac = fieldText(document, "ap_mac", "access_point_mac");
	info.client_ip = fieldText(document, "client_ip");
	const flash = findField(document, "flash_size");
	if (flash) {
		info.flash_size_bytes = numericFieldValue(flash) ?? void 0;
		const real = flash.value.match(/\(\s*real\s*:\s*([^)]+)\)/iu);
		if (real) info.flash_real_size_bytes = parseByteCount(real[1]) ?? void 0;
	}
	const usage = findField(document, "program_usage", "program_usage_percent");
	if (usage) info.program_usage_percent = usage.number ?? usage.integer ?? strictNumber(usage.value.replace(/%$/u, "").trim()) ?? void 0;
	const settings = fieldText(document, "settings_hash");
	if (settings) {
		const [hash, status] = settings.split(",", 2);
		info.settings_hash = hash.trim();
		if (status) info.settings_status = status.trim().replace(/^status\s*:/iu, "").trim().toLowerCase();
	}
	const heap = findField(document, "heap_memory", "heap_free", "free_heap");
	if (heap) info.heap_free_bytes = numericFieldValue(heap) ?? void 0;
	info.build = fieldText(document, "build");
	const buildPrefix = info.build?.split(" by ", 1)[0];
	if (buildPrefix) info.build_time = parseWireTime(buildPrefix) ?? void 0;
	const uptime = findField(document, "uptime", "uptime_seconds");
	if (uptime) info.uptime_seconds = uptime.duration_seconds ?? uptime.integer ?? parseUptime(uptime.value) ?? void 0;
	info.reported_state = fieldText(document, "state").toLowerCase();
}
function projectSignal(info, document) {
	const rssi = findField(document, "rssi", "signal", "signal_strength", "line_1");
	if (rssi) info.rssi_dbm = rssi.integer ?? strictInteger(rssi.value.replace(/dbm$/iu, "").trim()) ?? void 0;
	info.signal_quality = fieldText(document, "quality", "signal_quality", "line_2");
}
function projectWiFi(info, document) {
	const connected = findField(document, "connected", "wifi_connected");
	if (connected) info.wifi_connected = connected.boolean ?? parseBoolean(connected.value) ?? void 0;
	setIfText(info, "ssid", fieldText(document, "ssid"));
	setIfText(info, "wifi_ip", fieldText(document, "ip", "wifi_ip"));
	const accessPointStatus = accessPointNetworkStatus(findField(document, "ap_status", "access_point_status"));
	if (accessPointStatus) info.access_point_status = accessPointStatus;
	const stationStatus = stationNetworkStatus(findField(document, "st_status", "station_status"));
	if (stationStatus) {
		info.station_status = stationStatus.label;
		if (info.wifi_connected === void 0 && stationStatus.connected !== void 0) info.wifi_connected = stationStatus.connected;
	}
	if (info.rssi_dbm === void 0) {
		const rssi = findField(document, "rssi", "signal");
		if (rssi) info.rssi_dbm = rssi.integer;
	}
}
function projectStationConfig(info, document) {
	setIfText(info, "ssid", fieldText(document, "ssid"));
	setIfText(info, "wifi_ip", fieldText(document, "ip", "wifi_ip"));
	setIfText(info, "station_netmask", fieldText(document, "netmask"));
	setIfText(info, "station_gateway", fieldText(document, "gw", "gateway"));
	setIfText(info, "station_dns", fieldText(document, "dns", "dns_servers"));
	const dhcp = findField(document, "dhcp");
	if (dhcp) info.station_dhcp = dhcp.boolean ?? parseBoolean(dhcp.value) ?? void 0;
}
function projectAccessPointConfig(info, document) {
	setIfText(info, "access_point_ssid", fieldText(document, "ssid"));
	setIfText(info, "access_point_ip", fieldText(document, "ip"));
	setIfText(info, "access_point_netmask", fieldText(document, "netmask"));
}
function stationNetworkStatus(field) {
	if (!field) return null;
	const code = field.integer ?? strictInteger(field.value);
	if (code !== null) switch (code) {
		case 0: return { label: "Unknown" };
		case 1: return {
			label: "Connected",
			connected: true
		};
		case 2: return {
			label: "Disconnected",
			connected: false
		};
		case 3: return {
			label: "Connecting",
			connected: false
		};
		case 4: return {
			label: "SSID not found",
			connected: false
		};
		case 5: return {
			label: "Wrong password",
			connected: false
		};
		case 6: return {
			label: "Connection failed",
			connected: false
		};
		case 7: return {
			label: "Idle",
			connected: false
		};
	}
	const value = field.value.trim();
	const connected = parseBoolean(value);
	return value ? {
		label: connected === true ? "Connected" : connected === false ? "Disconnected" : value,
		...connected === null ? {} : { connected }
	} : null;
}
function accessPointNetworkStatus(field) {
	if (!field) return null;
	const code = field.integer ?? strictInteger(field.value);
	if (code === 1) return "Enabled";
	if (code === 2) return "Disabled";
	if (code === 0) return "Unknown";
	const value = field.value.trim();
	switch (value.toLowerCase()) {
		case "ok":
		case "enabled":
		case "on": return "Enabled";
		case "disconnected":
		case "disabled":
		case "off": return "Disabled";
		case "unknown":
		case "undefined": return "Unknown";
		default: return value || null;
	}
}
function setIfText(info, key, value) {
	if (value) info[key] = value;
}
function projectUpdate(info, document) {
	const hash = fieldText(document, "hash", "firmware_hash");
	const size = findField(document, "size", "firmware_size", "size_bytes");
	const free = findField(document, "free", "free_space", "free_bytes");
	const build = fieldText(document, "date", "build", "build_date");
	const date = findField(document, "date", "build_date");
	info.update = {
		...hash ? { hash } : {},
		...size && numericFieldValue(size) !== null ? { size_bytes: numericFieldValue(size) ?? void 0 } : {},
		...free && numericFieldValue(free) !== null ? { free_bytes: numericFieldValue(free) ?? void 0 } : {},
		...build ? { build } : {},
		...date && (date.time || parseWireTime(date.value)) ? { date: date.time || parseWireTime(date.value) || void 0 } : {}
	};
}
function projectDescription(info, document) {
	info.description = {
		friendly_name: fieldText(document, "friendly_name"),
		model_name: fieldText(document, "model_name"),
		model_number: fieldText(document, "model_number"),
		manufacturer: fieldText(document, "manufacturer"),
		manufacturer_url: fieldText(document, "manufacturer_url"),
		serial_number: fieldText(document, "serial_number"),
		udn: fieldText(document, "udn")
	};
}
function findField(document, ...keys) {
	for (const key of keys.map(canonicalKey)) {
		const field = document.fields.find((candidate) => candidate.key === key);
		if (field) return field;
	}
}
function fieldText(document, ...keys) {
	const field = findField(document, ...keys);
	return field && !field.redacted ? field.value.trim() : "";
}
function numericFieldValue(field) {
	return field.integer ?? parseByteCount(field.value);
}
function isSignalKey(key) {
	return key === "rssi" || key === "signal" || key === "signal_strength" || key.endsWith("_rssi");
}
function isPercentKey(key) {
	return key.includes("percent") || key === "program_usage" || key.endsWith("_usage");
}
function isByteKey(endpoint, key) {
	if (key.endsWith("_bytes")) return true;
	return (endpoint === "/info" || endpoint === "/update/info") && [
		"flash_size",
		"flash_real_size",
		"heap_memory",
		"free_heap",
		"heap_free",
		"size",
		"free",
		"firmware_size",
		"free_space"
	].includes(key);
}
function isBooleanKey(key) {
	return [
		"connected",
		"available",
		"valid",
		"corrupted",
		"protocol_match"
	].includes(key) || key.startsWith("is_") || key.startsWith("has_");
}
function isTimeKey(key) {
	return [
		"time",
		"date",
		"build_time",
		"fetched_at"
	].includes(key) || key.endsWith("_time") || key.endsWith("_at");
}
function isMACKey(key) {
	return key === "mac" || key.endsWith("_mac");
}
function isIPKey(key) {
	return key === "ip" || key.endsWith("_ip");
}
function isHashKey(key) {
	return key.includes("hash") || key.endsWith("_chip_id");
}
function isIntegerKey(key) {
	return [
		"count",
		"clients",
		"attempt",
		"revision",
		"code"
	].some((marker) => key.includes(marker));
}
function parseByteCount(value) {
	const match = value.match(/^\s*([+-]?(?:\d+(?:\.\d*)?|\.\d+))\s*(B|KB|MB|GB|KIB|MIB|GIB)?\b/iu);
	if (!match) return null;
	const number = Number(match[1]);
	if (!Number.isFinite(number) || number < 0) return null;
	const unit = (match[2] ?? "B").toUpperCase();
	const result = Math.round(number * (unit === "KB" || unit === "KIB" ? 1024 : unit === "MB" || unit === "MIB" ? 1024 ** 2 : unit === "GB" || unit === "GIB" ? 1024 ** 3 : 1));
	return Number.isSafeInteger(result) ? result : null;
}
function parseUptime(value) {
	const match = value.match(/^\s*(?:(\d+)\s+days?,\s*)?(\d{1,2}):(\d{2}):(\d{2})\s*$/iu);
	if (!match) return null;
	const days = Number(match[1] ?? 0);
	const hours = Number(match[2]);
	const minutes = Number(match[3]);
	const seconds = Number(match[4]);
	if (hours > 23 || minutes > 59 || seconds > 59) return null;
	return days * 86400 + hours * 3600 + minutes * 60 + seconds;
}
function parseBoolean(value) {
	switch (value.trim().toLowerCase()) {
		case "1":
		case "true":
		case "yes":
		case "on":
		case "enabled":
		case "connected":
		case "ok":
		case "valid": return true;
		case "0":
		case "false":
		case "no":
		case "off":
		case "disabled":
		case "disconnected":
		case "invalid": return false;
		default: return null;
	}
}
function parseWireTime(value) {
	const trimmed = value.trim();
	let parsed = Date.parse(trimmed);
	if (!Number.isNaN(parsed)) return new Date(parsed).toISOString();
	const firmware = trimmed.match(/^(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)\s+(\d{1,2})\s+(\d{4})\s+(\d{2}):(\d{2}):(\d{2})$/u);
	if (!firmware) return null;
	const month = [
		"Jan",
		"Feb",
		"Mar",
		"Apr",
		"May",
		"Jun",
		"Jul",
		"Aug",
		"Sep",
		"Oct",
		"Nov",
		"Dec"
	].indexOf(firmware[1]);
	parsed = Date.UTC(Number(firmware[3]), month, Number(firmware[2]), Number(firmware[4]), Number(firmware[5]), Number(firmware[6]));
	return new Date(parsed).toISOString();
}
function strictInteger(value) {
	if (!/^[+-]?\d+$/u.test(value)) return null;
	const number = Number(value);
	return Number.isSafeInteger(number) ? number : null;
}
function strictNumber(value) {
	if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/u.test(value)) return null;
	const number = Number(value);
	return Number.isFinite(number) ? number : null;
}
function isIPAddress(value) {
	if (/^(?:\d{1,3}\.){3}\d{1,3}$/u.test(value)) return value.split(".").every((part) => Number(part) <= 255);
	return value.includes(":") && /^[0-9a-f:.]+$/iu.test(value);
}
function jsonScalar(value) {
	if (value === null) return "null";
	if (typeof value === "string") return value;
	if (typeof value === "number" || typeof value === "boolean") return String(value);
	return String(value);
}
function isPlainRecord(value) {
	return value !== null && typeof value === "object" && !Array.isArray(value);
}
function stripLeadingBOM(value) {
	return value.startsWith("﻿") ? value.slice(1) : value;
}
function utf8ByteLength(value) {
	return new TextEncoder().encode(value).byteLength;
}
function firstSeparatorIndex(value) {
	const candidates = [value.indexOf(":"), value.indexOf("=")].filter((index) => index >= 0);
	return candidates.length > 0 ? Math.min(...candidates) : -1;
}
function validDate(value) {
	return Number.isNaN(value.valueOf()) ? /* @__PURE__ */ new Date() : value;
}
function errorMessage$2(error) {
	return error instanceof Error ? error.message : "invalid data";
}
//#endregion
//#region src/connection-preferences.ts
var storageKey = "rayanlamp.web.connection.v1";
var channelName = "rayanlamp-web-connection-v1";
var localEventName = "rayanlamp:connection-preferences";
function isStaticExport() {
	return false;
}
function defaultConnectionPreferences() {
	return {
		mode: isStaticExport() ? "direct" : "auto",
		lampURL: "http://rayanlamp.local"
	};
}
function readConnectionPreferences() {
	const fallback = defaultConnectionPreferences();
	try {
		const raw = localStorage.getItem(storageKey);
		if (!raw) return fallback;
		const value = JSON.parse(raw);
		return {
			mode: isConnectionMode(value.mode) ? value.mode : fallback.mode,
			lampURL: typeof value.lampURL === "string" && value.lampURL.trim() ? normalizeLampBaseURL(value.lampURL) : fallback.lampURL
		};
	} catch {
		return fallback;
	}
}
function saveConnectionPreferences(value) {
	const normalized = {
		mode: value.mode,
		lampURL: normalizeLampBaseURL(value.lampURL)
	};
	try {
		localStorage.setItem(storageKey, JSON.stringify(normalized));
	} catch {}
	window.dispatchEvent(new CustomEvent(localEventName, { detail: normalized }));
	if (typeof BroadcastChannel === "function") {
		const channel = new BroadcastChannel(channelName);
		channel.postMessage(normalized);
		channel.close();
	}
}
function subscribeConnectionPreferences(callback) {
	const onLocal = (event) => {
		const detail = event.detail;
		if (validPreferences(detail)) callback(detail);
	};
	window.addEventListener(localEventName, onLocal);
	const channel = typeof BroadcastChannel === "function" ? new BroadcastChannel(channelName) : null;
	const onBroadcast = (event) => {
		if (!validPreferences(event.data)) return;
		let value;
		try {
			value = {
				mode: event.data.mode,
				lampURL: normalizeLampBaseURL(event.data.lampURL)
			};
		} catch {
			return;
		}
		try {
			localStorage.setItem(storageKey, JSON.stringify(value));
		} catch {}
		callback(value);
	};
	channel?.addEventListener("message", onBroadcast);
	return () => {
		window.removeEventListener(localEventName, onLocal);
		channel?.removeEventListener("message", onBroadcast);
		channel?.close();
	};
}
function isConnectionMode(value) {
	return value === "auto" || value === "relay" || value === "direct";
}
function validPreferences(value) {
	if (!value || typeof value !== "object") return false;
	const candidate = value;
	return isConnectionMode(candidate.mode) && typeof candidate.lampURL === "string" && candidate.lampURL.trim().length > 0;
}
//#endregion
//#region src/direct-api.ts
var directConfigStorageKey = "rayanlamp.web.direct.config.v1";
var maxControlResponseBytes = 65536;
var maxDiagnosticResponseBytes = 1 << 20;
/**
* Browser-only client for firmware that exposes the audited Rayan Lamp HTTP
* and WebSocket surface. It deliberately never sends credentials or custom
* request headers, keeping cross-origin device reads in the CORS-simple path.
*/
var DirectLampAPI = class {
	kind = "direct";
	displayName = "Direct to lamp";
	baseURL;
	config;
	revision;
	current;
	diagnosticsSnapshot;
	projectionDocuments = {};
	fetcher;
	pageURL;
	clock;
	audio;
	socket = null;
	constructor(rawBaseURL, options = {}) {
		this.baseURL = normalizeLampBaseURL(rawBaseURL);
		this.fetcher = options.fetch ?? fetch.bind(globalThis);
		this.pageURL = options.pageURL;
		this.clock = options.now ?? (() => /* @__PURE__ */ new Date());
		this.audio = options.audio ?? browserAudio;
		this.config = loadDirectConfig(this.baseURL);
		this.revision = loadDirectRevision();
		this.current = offlineSnapshot(this.baseURL);
		this.diagnosticsSnapshot = emptyDiagnostics$1(this.baseURL);
	}
	get syncKey() {
		return `direct:${this.baseURL}`;
	}
	async establishSession(_force = false) {
		return "";
	}
	async bootstrap() {
		await this.repairMissingCustomAudioSelection();
		try {
			await this.status();
		} catch {}
		try {
			await this.refreshUIDiagnostics();
		} catch {}
		return {
			snapshot: clone(this.current),
			config: clone(this.config),
			version: {
				name: "RayanLampWeb",
				display_name: "Rayan Lamp Web",
				version: "1.0.0",
				company: "YektaYar",
				description: "Portable browser controller connected directly to the lamp",
				edition: buildEdition
			},
			revision: this.revision,
			capabilities: {
				lamp_control: true,
				realtime_events: true,
				websocket_events: this.config.lamp.websocket,
				transport_direct: true,
				transport_relay: false,
				native_shell: false,
				window_control: false,
				configuration: true,
				diagnostics: true,
				notifications: false,
				audio_sfx: true,
				global_hotkeys: false,
				hotkey_capture: false,
				hotkey_probe: false,
				developer_tools: true,
				automation: false,
				device_terminal: false
			},
			diagnostics: clone(this.diagnosticsSnapshot)
		};
	}
	async getConfig() {
		this.refreshStoredConfig();
		return {
			config: clone(this.config),
			revision: this.revision
		};
	}
	async patchConfig(patch, revision) {
		this.refreshStoredConfig();
		if (revision !== void 0 && revision !== "" && String(revision) !== String(this.revision)) throw new APIError(412, "stale_revision", "These browser settings changed in another tab. Reload and try again.");
		const candidate = mergeRecord$1(this.config, patch);
		validateDirectConfig(candidate);
		const oldBaseURL = this.baseURL;
		this.baseURL = normalizeLampBaseURL(candidate.lamp.base_url);
		candidate.lamp.base_url = this.baseURL;
		candidate.diagnostics.safe_endpoints = uniqueAuditedEndpoints(candidate.diagnostics.safe_endpoints);
		candidate.diagnostics.expert_enabled = false;
		candidate.diagnostics.expert_endpoints = [];
		this.config = clone(candidate);
		this.revision++;
		this.current = {
			...this.current,
			device_url: this.baseURL
		};
		if (oldBaseURL !== this.baseURL) {
			this.current = offlineSnapshot(this.baseURL);
			this.diagnosticsSnapshot = emptyDiagnostics$1(this.baseURL);
			this.projectionDocuments = {};
		}
		persistDirectConfig(this.config, this.revision);
		if (patch.lamp !== void 0) saveConnectionPreferences({
			...readConnectionPreferences(),
			lampURL: this.baseURL
		});
		this.broadcastConfig();
		return {
			config: clone(this.config),
			revision: this.revision
		};
	}
	async beginHotkeyCapture() {
		throw nativeOnly("Global hotkey recording");
	}
	async renewHotkeyCapture(_leaseID) {
		throw nativeOnly("Global hotkey recording");
	}
	async releaseHotkeyCapture(_leaseID, _keepalive = false) {
		throw nativeOnly("Global hotkey recording");
	}
	async probeHotkey(_leaseID, _chord) {
		throw nativeOnly("Global hotkey probing");
	}
	async integrationRegistrationStatus() {
		throw nativeOnly("Windows executable registration");
	}
	async updateIntegrationRegistration(_method) {
		throw nativeOnly("Windows executable registration");
	}
	async status() {
		try {
			const state = parseLampStatePayload(await this.requestText(directLampCommandURL(this.baseURL, "get"), maxControlResponseBytes));
			if (state === "unknown") throw new APIError(502, "invalid_lamp_state", "The lamp returned a response whose state was not recognized.");
			this.applyHTTPState(state, "http");
			return clone(this.current);
		} catch (error) {
			this.applyHTTPFailure(error);
			throw error;
		}
	}
	async command(command) {
		if (command === "window/show" || command === "window/hide") throw nativeOnly("Native window control");
		if (command === "refresh") return this.status();
		if (command === "reconnect") {
			this.socket?.close(4e3, "manual reconnect");
			return clone(this.current);
		}
		let resolved;
		if (command === "toggle") {
			if (this.current.state === "unknown" || !this.current.http_reachable && !this.current.websocket_online) await this.status();
			if (this.current.state === "unknown") throw new APIError(409, "state_unknown", "The lamp state is unknown, so a safe toggle cannot be chosen.");
			resolved = this.current.state === "on" ? "off" : "on";
		} else resolved = command;
		this.current = {
			...this.current,
			pending_command: resolved
		};
		try {
			const reported = parseLampStatePayload(await this.requestText(directLampCommandURL(this.baseURL, resolved), maxControlResponseBytes));
			this.applyHTTPState(reported === "unknown" ? resolved : reported, `http:${resolved}`);
			return clone(this.current);
		} catch (error) {
			this.applyHTTPFailure(error);
			throw error;
		}
	}
	async diagnostics(endpoint) {
		if (!isDirectDiagnosticEndpoint(endpoint)) throw new APIError(400, "endpoint_not_allowed", "Only audited public lamp diagnostic endpoints may be requested.");
		const content = await this.requestText(directDiagnosticURL(this.baseURL, endpoint), maxDiagnosticResponseBytes);
		return {
			endpoint,
			content,
			bytes: utf8Bytes(content)
		};
	}
	async uiDiagnostics() {
		return clone(this.diagnosticsSnapshot);
	}
	async refreshUIDiagnostics(endpoints) {
		const requested = normalizeDiagnosticSelection(endpoints);
		const attemptedAt = this.clock().toISOString();
		this.diagnosticsSnapshot = {
			...this.diagnosticsSnapshot,
			refreshing: true,
			last_attempt: attemptedAt
		};
		const refreshProjectionSources = diagnosticSelectionIsFull(requested);
		const [outcomes, projectionOutcomes] = await Promise.all([Promise.all(requested.map(async (endpoint) => {
			try {
				const content = await this.requestText(directDiagnosticURL(this.baseURL, endpoint), maxDiagnosticResponseBytes);
				const fetchedAt = this.clock();
				return {
					endpoint,
					result: {
						document: parseDiagnosticDocument(endpoint, content, fetchedAt),
						last_attempt: fetchedAt.toISOString(),
						last_success: fetchedAt.toISOString()
					}
				};
			} catch (error) {
				const previous = this.diagnosticsSnapshot.endpoints[endpoint];
				return {
					endpoint,
					result: {
						document: previous?.document ?? emptyDiagnosticDocument(endpoint, this.clock()),
						last_attempt: this.clock().toISOString(),
						...previous?.last_success ? { last_success: previous.last_success } : {},
						error: errorMessage$1(error),
						stale: Boolean(previous?.last_success)
					}
				};
			}
		})), refreshProjectionSources ? Promise.all(directProjectionDiagnosticURLs(this.baseURL).map(async ({ endpoint, url }) => {
			try {
				return {
					endpoint,
					document: parseDirectProjectionDiagnostic(endpoint, await this.requestText(url, maxDiagnosticResponseBytes), this.clock())
				};
			} catch {
				return { endpoint };
			}
		})) : Promise.resolve([])]);
		const nextEndpoints = { ...this.diagnosticsSnapshot.endpoints };
		let successes = 0;
		for (const outcome of outcomes) {
			nextEndpoints[outcome.endpoint] = outcome.result;
			if (!("error" in outcome.result)) successes++;
		}
		let projectionFailures = 0;
		for (const outcome of projectionOutcomes) if (outcome.document) this.projectionDocuments[outcome.endpoint] = outcome.document;
		else projectionFailures++;
		const documents = { ...this.projectionDocuments };
		for (const endpoint of DIRECT_DIAGNOSTIC_ENDPOINTS) {
			const result = nextEndpoints[endpoint];
			if (result?.last_success) documents[endpoint] = result.document;
		}
		const finishedAt = this.clock().toISOString();
		this.diagnosticsSnapshot = {
			device_url: this.baseURL,
			device: diagnosticDeviceFromDocuments(documents),
			endpoints: nextEndpoints,
			refreshing: false,
			last_attempt: attemptedAt,
			...successes > 0 ? { updated_at: finishedAt } : this.diagnosticsSnapshot.updated_at ? { updated_at: this.diagnosticsSnapshot.updated_at } : {}
		};
		this.broadcastDiagnostics();
		const failures = outcomes.length - successes + projectionFailures;
		const sourceCount = outcomes.length + projectionOutcomes.length;
		return {
			diagnostics: clone(this.diagnosticsSnapshot),
			...failures > 0 ? { warning: `${failures} of ${sourceCount} diagnostic sources could not be refreshed.` } : {}
		};
	}
	async notify(_title, _message) {
		throw nativeOnly("Native toast and balloon delivery");
	}
	async playAudio(cue = "message", count = 1) {
		this.refreshStoredConfig();
		await this.repairMissingCustomAudioSelection();
		await this.audio.play(cue, this.config.notifications, count);
		return {
			cue,
			count,
			played: true
		};
	}
	async importAudio(file, revision) {
		const asset = await this.audio.importCustomWAV(file);
		return this.patchConfig({ notifications: {
			sound: true,
			sound_source: "custom",
			sound_file: asset.id,
			sound_name: asset.name
		} }, revision);
	}
	realtimeURL() {
		return directWebSocketURL(this.baseURL);
	}
	realtimeOpened(socket) {
		this.socket = socket;
		this.current = {
			...this.current,
			websocket_online: true,
			reconnect_attempt: 0,
			last_connected: this.clock().toISOString(),
			last_error: void 0
		};
		socket.send("get");
		return [{
			type: "snapshot",
			snapshot: clone(this.current)
		}];
	}
	realtimeClosed() {
		this.socket = null;
		this.current = {
			...this.current,
			websocket_online: false,
			reconnect_attempt: this.current.reconnect_attempt + 1
		};
		return [{
			type: "snapshot",
			snapshot: clone(this.current)
		}];
	}
	decodeRealtimeMessage(raw) {
		const event = parseDirectEvent(raw);
		if (!event) return [];
		const now = this.clock().toISOString();
		this.current = {
			...this.current,
			websocket_online: true,
			reconnect_attempt: 0,
			last_event: event.raw,
			last_source: "websocket"
		};
		if (event.type === "snapshot") {
			const changed = this.current.state !== event.state;
			this.current = {
				...this.current,
				state: event.state,
				...changed ? { last_changed: now } : {}
			};
			return [{
				type: "snapshot",
				snapshot: clone(this.current)
			}];
		}
		if (event.type === "message") return [{
			type: "message",
			title: event.title,
			message: event.message,
			result: {
				kind: "message",
				icon: event.icon
			}
		}];
		return [{
			type: "message",
			title: "Rayan Lamp",
			message: event.count === 1 ? "The lamp requested an audible cue." : `The lamp requested ${event.count} audible cues.`,
			result: {
				kind: "beep",
				count: event.count
			}
		}];
	}
	applyHTTPState(state, source) {
		const now = this.clock().toISOString();
		const changed = this.current.state !== state;
		this.current = {
			...this.current,
			state,
			http_reachable: true,
			device_url: this.baseURL,
			last_event: state.toUpperCase(),
			last_source: source,
			last_error: void 0,
			pending_command: void 0,
			...changed ? { last_changed: now } : {}
		};
	}
	applyHTTPFailure(error) {
		this.current = {
			...this.current,
			http_reachable: false,
			device_url: this.baseURL,
			pending_command: void 0,
			last_error: errorMessage$1(error)
		};
	}
	async requestText(url, maximumBytes) {
		const pageError = directPageAccessError(this.pageURL ?? window.location.href, this.baseURL);
		if (pageError) throw pageError;
		const controller = new AbortController();
		const timeout = parseDurationMilliseconds(this.config.lamp.request_timeout) ?? 5e3;
		const timer = window.setTimeout(() => controller.abort(), timeout);
		try {
			const response = await this.fetcher(url, {
				method: "GET",
				mode: "cors",
				credentials: "omit",
				cache: "no-store",
				redirect: "error",
				signal: controller.signal
			});
			if (!response.ok) throw new APIError(response.status, `lamp_http_${response.status}`, `The lamp returned HTTP ${response.status}.`);
			const declared = Number(response.headers.get("Content-Length") ?? "0");
			if (Number.isFinite(declared) && declared > maximumBytes) throw new APIError(413, "response_too_large", "The lamp response exceeded the browser safety limit.");
			const content = await response.text();
			if (utf8Bytes(content) > maximumBytes) throw new APIError(413, "response_too_large", "The lamp response exceeded the browser safety limit.");
			return content;
		} catch (error) {
			if (isAbortError(error)) throw new APIError(408, "lamp_timeout", `The lamp did not respond within ${this.config.lamp.request_timeout}.`);
			if (error instanceof APIError) throw error;
			throw new APIError(0, "lamp_network_error", "The browser could not reach the lamp. Check its address, local-network access, and CORS support.");
		} finally {
			window.clearTimeout(timer);
		}
	}
	broadcastConfig() {
		this.broadcast({
			kind: "config",
			config: clone(this.config),
			revision: this.revision
		});
	}
	async repairMissingCustomAudioSelection() {
		const notifications = this.config.notifications;
		if (notifications.sound_source !== "custom") return;
		let available = false;
		try {
			available = await this.audio.describe(notifications.sound_file) !== null;
		} catch {}
		if (available) return;
		this.config = {
			...this.config,
			notifications: {
				...notifications,
				sound_source: "bundled",
				sound_file: "",
				sound_name: ""
			}
		};
		this.revision++;
		persistDirectConfig(this.config, this.revision);
		this.broadcastConfig();
	}
	refreshStoredConfig() {
		const storedRevision = loadDirectRevision();
		if (storedRevision <= this.revision) return;
		this.config = loadDirectConfig(this.baseURL);
		this.revision = storedRevision;
	}
	broadcastDiagnostics() {
		this.broadcast({
			kind: "diagnostics",
			diagnostics: clone(this.diagnosticsSnapshot)
		});
	}
	broadcast(payload) {
		if (typeof BroadcastChannel !== "function") return;
		const channel = new BroadcastChannel(syncChannelName(this.syncKey));
		channel.postMessage({
			protocol: 1,
			sender: `direct-client-${randomID()}`,
			connected: this.current.websocket_online,
			...payload
		});
		channel.close();
	}
};
function directPageAccessError(pageURL, lampURL) {
	let page;
	let lamp;
	try {
		page = new URL(pageURL);
		lamp = new URL(normalizeLampBaseURL(lampURL));
	} catch {
		return new APIError(400, "invalid_url", "The dashboard or lamp URL is invalid.");
	}
	if (page.protocol === "file:") return new APIError(0, "file_origin_unsupported", "Serve the exported ZIP from a local HTTP server; browsers do not reliably allow lamp access from file:// pages.");
	if (page.protocol === "https:" && lamp.protocol === "http:") return new APIError(0, "mixed_content", "This HTTPS page cannot contact an HTTP lamp. Serve the dashboard over HTTP on the local network, or enable HTTPS on the lamp.");
	return null;
}
function defaultDirectConfig(baseURL) {
	return {
		lamp: {
			base_url: baseURL,
			request_timeout: "5s",
			websocket: true,
			reconnect_min: "1s",
			reconnect_max: "30s",
			http_poll: "30s",
			telnet_port: 23
		},
		ui: {
			view_mode: "compact",
			theme: "system",
			locale: "auto",
			backdrop: "acrylic",
			render_backend: "auto",
			rounded_corners: true,
			layered_foreground: true,
			title_bar_mode: "none",
			border_mode: "auto",
			settings_tab: 0,
			settings_bounds: {
				x: -1,
				y: -1,
				width: 780,
				height: 640
			},
			show_title_bar: false,
			always_on_top: false,
			close_to_tray: true,
			prewarm_tray_menu: true,
			start_minimized: false,
			window_visible: true,
			window_state: "normal",
			show_taskbar: true,
			animations: true,
			magnetic_snap: false,
			magnetic_snap_distance: 16,
			window_bounds: {
				x: -1,
				y: -1,
				width: 176,
				height: 176
			}
		},
		notifications: {
			enabled: false,
			state_changes: false,
			initial_sync: false,
			custom_message: false,
			sound: true,
			sound_source: "bundled",
			sound_file: "",
			sound_name: "",
			delivery: "auto"
		},
		hotkeys: {
			toggle: "",
			on: "",
			off: "",
			show: ""
		},
		automation: {
			enabled: false,
			on_script: "",
			off_script: "",
			message_script: "",
			lock_script: "",
			unlock_script: "",
			suspend_script: "",
			resume_script: "",
			network_change_script: "",
			working_directory: "",
			timeout: "15s",
			capture_output: false
		},
		integration: {
			start_with_windows: false,
			local_api_enabled: false,
			local_api_listen: ""
		},
		diagnostics: {
			safe_endpoints: [...DIRECT_DIAGNOSTIC_ENDPOINTS],
			expert_enabled: false,
			expert_endpoints: []
		}
	};
}
function loadDirectConfig(baseURL) {
	const fallback = defaultDirectConfig(baseURL);
	try {
		const raw = localStorage.getItem(directConfigStorageKey);
		if (!raw) return fallback;
		const merged = mergeRecord$1(fallback, JSON.parse(raw));
		merged.lamp.base_url = baseURL;
		validateDirectConfig(merged);
		merged.diagnostics.safe_endpoints = uniqueAuditedEndpoints(merged.diagnostics.safe_endpoints);
		merged.diagnostics.expert_enabled = false;
		merged.diagnostics.expert_endpoints = [];
		return merged;
	} catch {
		return fallback;
	}
}
function loadDirectRevision() {
	try {
		const value = Number(localStorage.getItem(`${directConfigStorageKey}.revision`) ?? "1");
		return Number.isSafeInteger(value) && value > 0 ? value : 1;
	} catch {
		return 1;
	}
}
function persistDirectConfig(config, revision) {
	try {
		localStorage.setItem(directConfigStorageKey, JSON.stringify(config));
		localStorage.setItem(`${directConfigStorageKey}.revision`, String(revision));
	} catch {}
}
function validateDirectConfig(config) {
	normalizeLampBaseURL(config.lamp.base_url);
	if (config.ui.theme !== "system" && config.ui.theme !== "light" && config.ui.theme !== "dark") throw new APIError(400, "invalid_config", "Color theme must be system, light, or dark.");
	if (config.ui.locale !== void 0 && config.ui.locale !== "auto" && config.ui.locale !== "en" && config.ui.locale !== "fa") throw new APIError(400, "invalid_config", "Interface language must be auto, en, or fa.");
	if (config.ui.window_state !== "normal" && config.ui.window_state !== "minimized" && config.ui.window_state !== "maximized") throw new APIError(400, "invalid_config", "Window state must be normal, minimized, or maximized.");
	if (config.notifications.sound_source !== "bundled" && config.notifications.sound_source !== "custom") throw new APIError(400, "invalid_config", "Sound source must be bundled or custom.");
	if (config.notifications.sound_file !== "" && !/^[a-f0-9]{64}$/.test(config.notifications.sound_file)) throw new APIError(400, "invalid_config", "Custom sound identity must be a lowercase SHA-256 value.");
	if (config.notifications.sound_source === "custom" && config.notifications.sound_file === "") throw new APIError(400, "invalid_config", "Import a valid custom WAV before selecting the custom sound source.");
	if (Array.from(config.notifications.sound_name).length > 255 || /[\u0000-\u001f\u007f]/.test(config.notifications.sound_name)) throw new APIError(400, "invalid_config", "Custom sound name is invalid.");
	if (!Number.isInteger(config.lamp.telnet_port) || config.lamp.telnet_port < 1 || config.lamp.telnet_port > 65535) throw new APIError(400, "invalid_config", "Device terminal port must be an integer between 1 and 65535.");
	const timeout = parseDurationMilliseconds(config.lamp.request_timeout);
	const reconnectMin = parseDurationMilliseconds(config.lamp.reconnect_min);
	const reconnectMax = parseDurationMilliseconds(config.lamp.reconnect_max);
	const poll = parseDurationMilliseconds(config.lamp.http_poll);
	if (timeout === null || timeout < 100 || timeout > 12e4) throw new APIError(400, "invalid_config", "Lamp request timeout must be between 100ms and 2m.");
	if (reconnectMin === null || reconnectMax === null || reconnectMin < 100 || reconnectMax < reconnectMin) throw new APIError(400, "invalid_config", "Reconnect timing must start at 100ms or more, and maximum must not be smaller than minimum.");
	if (poll === null || poll !== 0 && poll < 1e3) throw new APIError(400, "invalid_config", "HTTP polling must be 0 to disable it, or at least 1s.");
	uniqueAuditedEndpoints(config.diagnostics.safe_endpoints);
	if (config.diagnostics.expert_endpoints.length > 0) throw new APIError(400, "endpoint_not_allowed", "Direct mode cannot enable unaudited expert endpoints.");
}
function uniqueAuditedEndpoints(endpoints) {
	const result = [];
	for (const endpoint of endpoints) {
		if (!isDirectDiagnosticEndpoint(endpoint)) throw new APIError(400, "endpoint_not_allowed", `Direct diagnostics endpoint ${JSON.stringify(endpoint)} is not audited.`);
		if (!result.includes(endpoint)) result.push(endpoint);
	}
	return result;
}
function normalizeDiagnosticSelection(endpoints) {
	return uniqueAuditedEndpoints(endpoints && endpoints.length > 0 ? endpoints : [...DIRECT_DIAGNOSTIC_ENDPOINTS]);
}
function diagnosticSelectionIsFull(endpoints) {
	return DIRECT_DIAGNOSTIC_ENDPOINTS.every((endpoint) => endpoints.includes(endpoint));
}
function emptyDiagnostics$1(baseURL) {
	return {
		device_url: baseURL,
		device: {},
		endpoints: {},
		refreshing: false
	};
}
function emptyDiagnosticDocument(endpoint, fetchedAt) {
	return parseDiagnosticDocument(endpoint, "", fetchedAt);
}
function offlineSnapshot(baseURL) {
	return {
		state: "unknown",
		http_reachable: false,
		websocket_online: false,
		device_url: baseURL,
		reconnect_attempt: 0
	};
}
function nativeOnly(feature) {
	return new APIError(501, "native_feature_unavailable", `${feature} is available when the dashboard connects through the Go coordinator.`);
}
function mergeRecord$1(base, patch) {
	if (!base || !patch || typeof base !== "object" || typeof patch !== "object" || Array.isArray(base) || Array.isArray(patch)) return patch;
	const output = { ...base };
	for (const [key, value] of Object.entries(patch)) {
		if (value === void 0) continue;
		const current = output[key];
		output[key] = current && value && typeof current === "object" && typeof value === "object" && !Array.isArray(current) && !Array.isArray(value) ? mergeRecord$1(current, value) : value;
	}
	return output;
}
function clone(value) {
	return JSON.parse(JSON.stringify(value));
}
function errorMessage$1(error) {
	return error instanceof Error ? error.message : "The lamp request failed.";
}
function isAbortError(error) {
	return error instanceof DOMException ? error.name === "AbortError" : Boolean(error && typeof error === "object" && "name" in error && error.name === "AbortError");
}
function utf8Bytes(value) {
	return new TextEncoder().encode(value).byteLength;
}
function syncChannelName(key) {
	let hash = 2166136261;
	for (let index = 0; index < key.length; index++) {
		hash ^= key.charCodeAt(index);
		hash = Math.imul(hash, 16777619);
	}
	return `rayanlamp-web-v1-${(hash >>> 0).toString(16).padStart(8, "0")}`;
}
function randomID() {
	return globalThis.crypto?.randomUUID?.() ?? String(Math.random());
}
//#endregion
//#region src/browser-console.ts
var eventPresentation = {
	"app.start": { emoji: "🚀" },
	"transport.ready": { emoji: "🔌" },
	"transport.error": { emoji: "⚠️" },
	"lamp.state": { emoji: "💡" },
	"lamp.command": { emoji: "⚡" },
	"websocket.connected": { emoji: "📡" },
	"websocket.disconnected": { emoji: "📡" },
	"message.received": { emoji: "💬" },
	"audio.cue": { emoji: "🔊" },
	"audio.error": { emoji: "🔇" },
	"device.event": { emoji: "📟" },
	"diagnostics.loaded": { emoji: "🩺" },
	"diagnostics.error": { emoji: "⚠️" },
	"settings.saved": { emoji: "✅" },
	"settings.error": { emoji: "⚠️" }
};
var levelPalette = {
	debug: {
		foreground: "#F3F8FB",
		background: "#17354A",
		detailForeground: "#A9BAC8",
		detailBackground: "#0B1724"
	},
	info: {
		foreground: "#07111C",
		background: "#22BEF2",
		detailForeground: "#F3F8FB",
		detailBackground: "#0B1724"
	},
	success: {
		foreground: "#07111C",
		background: "#4BDA97",
		detailForeground: "#F3F8FB",
		detailBackground: "#102334"
	},
	warn: {
		foreground: "#07111C",
		background: "#FFD35B",
		detailForeground: "#F3F8FB",
		detailBackground: "#102334"
	},
	error: {
		foreground: "#07111C",
		background: "#FF677D",
		detailForeground: "#F3F8FB",
		detailBackground: "#102334"
	}
};
var metadataAllowlist = keys("source", "action", "state", "attempt", "elapsed_ms", "count", "endpoint", "bytes", "error_code");
var sensitiveKey = /(?:^|[_-])(?:authorization|cookie|credential|csrf|password|passwd|passphrase|secret|session|token|api[_-]?key)(?:$|[_-])/i;
var jwtPattern = /\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b/g;
var bearerPattern = /\bBearer\s+[A-Za-z0-9._~+/-]+=*/gi;
var credentialURLPattern = /(\b[a-z][a-z0-9+.-]*:\/\/)[^/\s:@]+:[^/\s@]+@/gi;
var sensitiveParameterPattern = /([?&](?:authorization|csrf|password|secret|session|token|api[_-]?key)=)[^&#\s]*/gi;
var unsafeConsoleCharacterPattern = /[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f\u202a-\u202e\u2066-\u2069]/g;
var maximumStringLength = 500;
var maximumDepth = 4;
var maximumArrayItems = 20;
var maximumObjectKeys = 30;
function keys(...values) {
	return new Set(values);
}
function redactText(value) {
	const clean = value.replace(/[\r\n\t]+/g, " ").replace(unsafeConsoleCharacterPattern, "").replace(credentialURLPattern, "$1[REDACTED]@").replace(bearerPattern, "Bearer [REDACTED]").replace(jwtPattern, "[REDACTED JWT]").replace(sensitiveParameterPattern, "$1[REDACTED]");
	return clean.length <= maximumStringLength ? clean : `${clean.slice(0, maximumStringLength)}…`;
}
function sanitizeValue(value, seen, depth) {
	if (value === null || typeof value === "boolean" || typeof value === "number") return value;
	if (typeof value === "string") return redactText(value);
	if (typeof value === "bigint") return `${value}n`;
	if (typeof value === "undefined") return void 0;
	if (typeof value === "symbol") return value.description ?? "symbol";
	if (typeof value === "function") return `[Function ${value.name || "anonymous"}]`;
	if (value instanceof Error) return sanitizeError(value);
	if (depth >= maximumDepth) return "[Maximum depth]";
	if (seen.has(value)) return "[Circular]";
	seen.add(value);
	try {
		if (Array.isArray(value)) {
			const result = value.slice(0, maximumArrayItems).map((item) => sanitizeValue(item, seen, depth + 1));
			if (value.length > maximumArrayItems) result.push(`[${value.length - maximumArrayItems} more items]`);
			return result;
		}
		if (ArrayBuffer.isView(value)) return `[${value.constructor.name} ${value.byteLength} bytes]`;
		if (value instanceof ArrayBuffer) return `[ArrayBuffer ${value.byteLength} bytes]`;
		if (value instanceof Date) return value.toISOString();
		if (typeof Element !== "undefined" && value instanceof Element) {
			const id = value.id ? `#${value.id}` : "";
			return `<${value.tagName.toLowerCase()}${id}>`;
		}
		const result = {};
		const entries = Object.entries(value).slice(0, maximumObjectKeys);
		for (const [key, item] of entries) result[key] = sensitiveKey.test(key) ? "[REDACTED]" : sanitizeValue(item, seen, depth + 1);
		if (Object.keys(value).length > maximumObjectKeys) result.omitted = Object.keys(value).length - maximumObjectKeys;
		return result;
	} finally {
		seen.delete(value);
	}
}
function sanitizeMetadata(metadata) {
	if (!metadata) return void 0;
	const result = {};
	for (const [key, value] of Object.entries(metadata)) {
		if (!metadataAllowlist.has(key)) continue;
		result[key] = sensitiveKey.test(key) ? "[REDACTED]" : sanitizeValue(value, /* @__PURE__ */ new WeakSet(), 0);
	}
	return Object.keys(result).length > 0 ? result : void 0;
}
function sanitizeError(error) {
	const extended = error;
	const result = {};
	if (typeof extended.code === "string" || typeof extended.code === "number") result.error_code = sanitizeValue(extended.code, /* @__PURE__ */ new WeakSet(), 0);
	else if (typeof extended.status === "number") result.error_code = `http_${extended.status}`;
	else result.error_code = redactText(error.name || "Error");
	return result;
}
function methodFor(sink, level) {
	switch (level) {
		case "debug": return sink.debug.bind(sink);
		case "info": return sink.info.bind(sink);
		case "success": return sink.info.bind(sink);
		case "warn": return sink.warn.bind(sink);
		case "error": return sink.error.bind(sink);
	}
}
function badgeStyle(palette) {
	return [
		`color:${palette.foreground}`,
		`background:${palette.background}`,
		"font:600 11px/1.8 ui-monospace,SFMono-Regular,Consolas,monospace",
		"letter-spacing:.045em",
		"padding:2px 8px",
		"border-radius:999px 0 0 999px"
	].join(";");
}
function detailStyle(palette) {
	return [
		`color:${palette.detailForeground}`,
		`background:${palette.detailBackground}`,
		"font:500 11px/1.8 system-ui,-apple-system,\"Segoe UI\",sans-serif",
		"padding:2px 9px",
		"border-radius:0 999px 999px 0"
	].join(";");
}
function brandStyle() {
	return [
		"color:#07111C",
		"background:linear-gradient(135deg,#A9ECFF,#24C8FF 52%,#FFD465)",
		"font:800 13px/2 system-ui,-apple-system,\"Segoe UI\",sans-serif",
		"letter-spacing:.08em",
		"padding:3px 11px",
		"border-radius:999px 0 0 999px"
	].join(";");
}
function brandDetailStyle() {
	return [
		"color:#F3F8FB",
		"background:#0B1724",
		"font:600 12px/2 system-ui,-apple-system,\"Segoe UI\",sans-serif",
		"padding:3px 11px",
		"border-radius:0 999px 999px 0"
	].join(";");
}
function resetStyle() {
	return "color:inherit;background:transparent;font:inherit;padding:0;border-radius:0";
}
var StyledBrowserConsole = class {
	sink;
	now;
	recent = /* @__PURE__ */ new Map();
	constructor(sink = console, now = () => Date.now()) {
		this.sink = sink;
		this.now = now;
	}
	start(metadata) {
		this.sink.groupCollapsed("%c 💡 RAYAN LAMP %c Browser · GUI · TUI activity contract%c", brandStyle(), brandDetailStyle(), resetStyle());
		this.event("info", "app.start", "Web controller initialized", metadata, {
			dedupeKey: "app.start",
			dedupeWindowMs: Number.POSITIVE_INFINITY
		});
		this.sink.debug("%c 🔐 PRIVACY %c Structured context is allowlisted and credentials are redacted%c", badgeStyle(levelPalette.debug), detailStyle(levelPalette.debug), resetStyle());
		this.sink.log("%c ✨ TIP %c Filter by event name, level, or source; heartbeats and unchanged polls stay quiet%c", badgeStyle(levelPalette.success), detailStyle(levelPalette.success), resetStyle());
		this.sink.groupEnd?.();
	}
	event(level, event, message, metadata, options = {}) {
		if (this.isDuplicate(options)) return;
		const presentation = eventPresentation[event];
		const palette = levelPalette[level];
		const call = methodFor(this.sink, level);
		const detail = redactText(message);
		const format = `%c ${presentation.emoji} ${level.toUpperCase()} · ${event} %c ${detail}%c`;
		const safe = sanitizeMetadata(metadata);
		const structured = {
			timestamp: new Date(this.now()).toISOString(),
			level,
			event,
			detail,
			...safe
		};
		call(format, badgeStyle(palette), detailStyle(palette), resetStyle(), structured);
	}
	isDuplicate(options) {
		if (!options.dedupeKey) return false;
		const windowMs = options.dedupeWindowMs ?? 1e3;
		const now = this.now();
		const previous = this.recent.get(options.dedupeKey);
		if (previous !== void 0 && now - previous < windowMs) return true;
		this.recent.set(options.dedupeKey, now);
		return false;
	}
};
function browserErrorMetadata(error) {
	if (error instanceof Error) return sanitizeError(error);
	return { error_code: "unknown_error" };
}
var browserConsole = new StyledBrowserConsole();
//#endregion
//#region src/client-factory.ts
/**
* Selects the same-origin Go coordinator when it is available, otherwise the
* browser talks to the configured lamp directly. Fallback is intentionally
* confined to "auto"; explicit relay/direct choices never change transport.
*/
var AutoRayanLampClient = class {
	relay;
	direct;
	active = null;
	constructor(relay, direct) {
		this.relay = relay;
		this.direct = direct;
	}
	get kind() {
		return this.active?.kind ?? "auto";
	}
	get displayName() {
		return this.active?.displayName ?? "Automatic connection";
	}
	get syncKey() {
		return this.active?.syncKey ?? `auto:${this.direct.syncKey}`;
	}
	async bootstrap() {
		if (this.active) return this.active.bootstrap();
		let relayError;
		try {
			const value = await this.relay.bootstrap();
			this.active = this.relay;
			return value;
		} catch (error) {
			relayError = error;
			browserConsole.event("warn", "transport.error", "Coordinator unavailable; trying the direct lamp transport", {
				source: "relay",
				action: "fallback-to-direct",
				...browserErrorMetadata(error)
			});
		}
		try {
			const value = await this.direct.bootstrap();
			this.active = this.direct;
			return value;
		} catch (directError) {
			throw new APIError(0, "automatic_connection_failed", `Neither connection mode is available. Go coordinator: ${messageOf(relayError)} Direct lamp: ${messageOf(directError)}`);
		}
	}
	establishSession(force = false) {
		return this.active?.establishSession(force) ?? Promise.resolve("");
	}
	getConfig() {
		return this.requireActive().getConfig();
	}
	patchConfig(patch, revision) {
		return this.requireActive().patchConfig(patch, revision);
	}
	beginHotkeyCapture() {
		return this.requireActive().beginHotkeyCapture();
	}
	renewHotkeyCapture(leaseID) {
		return this.requireActive().renewHotkeyCapture(leaseID);
	}
	releaseHotkeyCapture(leaseID, keepalive = false) {
		return this.requireActive().releaseHotkeyCapture(leaseID, keepalive);
	}
	probeHotkey(leaseID, chord) {
		return this.requireActive().probeHotkey(leaseID, chord);
	}
	integrationRegistrationStatus() {
		return this.requireActive().integrationRegistrationStatus();
	}
	updateIntegrationRegistration(method) {
		return this.requireActive().updateIntegrationRegistration(method);
	}
	status() {
		return this.requireActive().status();
	}
	command(command) {
		return this.requireActive().command(command);
	}
	diagnostics(endpoint) {
		return this.requireActive().diagnostics(endpoint);
	}
	uiDiagnostics() {
		return this.requireActive().uiDiagnostics();
	}
	refreshUIDiagnostics(endpoints) {
		return this.requireActive().refreshUIDiagnostics(endpoints);
	}
	notify(title, message) {
		return this.requireActive().notify(title, message);
	}
	playAudio(cue = "message", count = 1) {
		return this.requireActive().playAudio(cue, count);
	}
	importAudio(file, revision) {
		return this.requireActive().importAudio(file, revision);
	}
	realtimeURL() {
		return this.requireActive().realtimeURL();
	}
	realtimeOpened(socket) {
		return this.requireActive().realtimeOpened(socket);
	}
	realtimeClosed() {
		return this.requireActive().realtimeClosed();
	}
	decodeRealtimeMessage(raw) {
		return this.requireActive().decodeRealtimeMessage(raw);
	}
	requireActive() {
		if (!this.active) throw new APIError(503, "connection_not_selected", "The automatic connection is still being selected.");
		return this.active;
	}
};
function createRayanLampClient(preferences) {
	if (preferences.mode === "relay") return new RayanLampAPI();
	if (preferences.mode === "direct") return new DirectLampAPI(preferences.lampURL);
	return new AutoRayanLampClient(new RayanLampAPI(), new DirectLampAPI(preferences.lampURL));
}
function messageOf(error) {
	return error instanceof Error ? error.message : "unavailable.";
}
//#endregion
//#region node_modules/@preact/signals-core/dist/signals-core.module.js
var i$1 = Symbol.for("preact-signals");
function t$1() {
	if (!(v > 1)) {
		var i, t = !1;
		(function() {
			var i = c;
			c = void 0;
			while (void 0 !== i) {
				var t = i.S;
				if (t.v === i.v) {
					for (var n = t.t; void 0 !== n; n = n.x) if (n.i === i.i) n.i = t.i;
				}
				i = i.o;
			}
		})();
		while (void 0 !== h$1) {
			var n = h$1;
			h$1 = void 0;
			s++;
			while (void 0 !== n) {
				var r = n.u;
				n.u = void 0;
				n.f &= -3;
				if (!(8 & n.f) && w$1(n)) try {
					n.c();
				} catch (n) {
					if (!t) {
						i = n;
						t = !0;
					}
				}
				n = r;
			}
		}
		s = 0;
		v--;
		if (t) throw i;
	} else v--;
}
function n(i) {
	if (v > 0) return i();
	e = ++u$1;
	v++;
	try {
		return i();
	} finally {
		t$1();
	}
}
var r;
var o = void 0;
function f$1(i) {
	var t = o, n = r;
	o = void 0;
	r = void 0;
	try {
		return i();
	} finally {
		o = t;
		r = n;
	}
}
var h$1 = void 0;
var v = 0;
var s = 0;
var u$1 = 0;
var e = 0;
var c = void 0;
var d$1 = 0;
function a(i) {
	if (void 0 !== o) {
		var t = i.n;
		if (void 0 === t || t.t !== o) {
			t = {
				i: 0,
				S: i,
				p: o.s,
				n: void 0,
				t: o,
				e: void 0,
				x: void 0,
				r: t
			};
			if (void 0 !== o.s) o.s.n = t;
			o.s = t;
			i.n = t;
			if (32 & o.f) i.S(t);
			return t;
		} else if (-1 === t.i) {
			t.i = 0;
			if (void 0 !== t.n) {
				t.n.p = t.p;
				if (void 0 !== t.p) t.p.n = t.n;
				t.p = o.s;
				t.n = void 0;
				o.s.n = t;
				o.s = t;
			}
			return t;
		}
	}
}
function l$1(i, t) {
	this.v = i;
	this.i = 0;
	this.n = void 0;
	this.t = void 0;
	this.l = 0;
	this.W = null == t ? void 0 : t.watched;
	this.Z = null == t ? void 0 : t.unwatched;
	this.name = null == t ? void 0 : t.name;
}
l$1.prototype.brand = i$1;
l$1.prototype.h = function() {
	return !0;
};
l$1.prototype.S = function(i) {
	var t = this, n = this.t;
	if (n !== i && void 0 === i.e) {
		i.x = n;
		this.t = i;
		if (void 0 !== n) n.e = i;
		else f$1(function() {
			var i;
			null == (i = t.W) || i.call(t);
		});
	}
};
l$1.prototype.U = function(i) {
	var t = this;
	if (void 0 !== this.t) {
		var n = i.e, r = i.x;
		if (void 0 !== n) {
			n.x = r;
			i.e = void 0;
		}
		if (void 0 !== r) {
			r.e = n;
			i.x = void 0;
		}
		if (i === this.t) {
			this.t = r;
			if (void 0 === r) f$1(function() {
				var i;
				null == (i = t.Z) || i.call(t);
			});
		}
	}
};
l$1.prototype.subscribe = function(i) {
	var t = this;
	return j(function() {
		var n = t.value;
		f$1(function() {
			return i(n);
		});
	}, { name: "sub" });
};
l$1.prototype.valueOf = function() {
	return this.value;
};
l$1.prototype.toString = function() {
	return this.value + "";
};
l$1.prototype.toJSON = function() {
	return this.value;
};
l$1.prototype.peek = function() {
	var i = this;
	return f$1(function() {
		return i.value;
	});
};
Object.defineProperty(l$1.prototype, "value", {
	get: function() {
		var i = a(this);
		if (void 0 !== i) i.i = this.i;
		return this.v;
	},
	set: function(i) {
		if (i !== this.v) {
			if (s > 100) throw new Error("Cycle detected");
			(function(i) {
				if (0 !== v && 0 === s) {
					if (i.l !== e) {
						i.l = e;
						c = {
							S: i,
							v: i.v,
							i: i.i,
							o: c
						};
					}
				}
			})(this);
			this.v = i;
			this.i++;
			d$1++;
			v++;
			try {
				for (var n = this.t; void 0 !== n; n = n.x) n.t.N();
			} finally {
				t$1();
			}
		}
	}
});
function y$1(i, t) {
	return new l$1(i, t);
}
function w$1(i) {
	for (var t = i.s; void 0 !== t; t = t.n) if (t.S.i !== t.i || !t.S.h() || t.S.i !== t.i) return !0;
	return !1;
}
function _$1(i) {
	for (var t = i.s; void 0 !== t; t = t.n) {
		var n = t.S.n;
		if (void 0 !== n) t.r = n;
		t.S.n = t;
		t.i = -1;
		if (void 0 === t.n) {
			i.s = t;
			break;
		}
	}
}
function b$1(i) {
	var t = i.s, n = void 0;
	while (void 0 !== t) {
		var r = t.p;
		if (-1 === t.i) {
			t.S.U(t);
			if (void 0 !== r) r.n = t.n;
			if (void 0 !== t.n) t.n.p = r;
		} else n = t;
		t.S.n = t.r;
		if (void 0 !== t.r) t.r = void 0;
		t = r;
	}
	i.s = n;
}
function p$1(i, t) {
	l$1.call(this, void 0, t);
	this.x = i;
	this.s = void 0;
	this.g = d$1 - 1;
	this.f = 4;
}
p$1.prototype = new l$1();
p$1.prototype.h = function() {
	this.f &= -3;
	if (1 & this.f) return !1;
	if (32 == (36 & this.f)) return !0;
	this.f &= -5;
	if (this.g === d$1) return !0;
	this.g = d$1;
	this.f |= 1;
	if (this.i > 0 && !w$1(this)) {
		this.f &= -2;
		return !0;
	}
	var i = o;
	try {
		_$1(this);
		o = this;
		var t = this.x();
		if (16 & this.f || this.v !== t || 0 === this.i) {
			this.v = t;
			this.f &= -17;
			this.i++;
		}
	} catch (i) {
		this.v = i;
		this.f |= 16;
		this.i++;
	}
	o = i;
	b$1(this);
	this.f &= -2;
	return !0;
};
p$1.prototype.S = function(i) {
	if (void 0 === this.t) {
		this.f |= 36;
		for (var t = this.s; void 0 !== t; t = t.n) t.S.S(t);
	}
	l$1.prototype.S.call(this, i);
};
p$1.prototype.U = function(i) {
	if (void 0 !== this.t) {
		l$1.prototype.U.call(this, i);
		if (void 0 === this.t) {
			this.f &= -33;
			for (var t = this.s; void 0 !== t; t = t.n) t.S.U(t);
		}
	}
};
p$1.prototype.N = function() {
	if (!(2 & this.f)) {
		this.f |= 6;
		for (var i = this.t; void 0 !== i; i = i.x) i.t.N();
	}
};
Object.defineProperty(p$1.prototype, "value", { get: function() {
	if (1 & this.f) throw new Error("Cycle detected");
	var i = a(this);
	this.h();
	if (void 0 !== i) i.i = this.i;
	if (16 & this.f) throw this.v;
	return this.v;
} });
function g$1(i, t) {
	return new p$1(i, t);
}
function S(i) {
	var n = i.m;
	i.m = void 0;
	if ("function" == typeof n) {
		v++;
		var r = o;
		o = void 0;
		try {
			n();
		} catch (t) {
			i.f &= -2;
			i.f |= 8;
			m(i);
			throw t;
		} finally {
			o = r;
			t$1();
		}
	}
}
function m(i) {
	for (var t = i.s; void 0 !== t; t = t.n) t.S.U(t);
	i.x = void 0;
	i.s = void 0;
	S(i);
}
function x$1(i) {
	if (o !== this) throw new Error("Out-of-order effect");
	b$1(this);
	o = i;
	this.f &= -2;
	if (8 & this.f) m(this);
	t$1();
}
function E(i, t) {
	this.x = i;
	this.m = void 0;
	this.s = void 0;
	this.u = void 0;
	this.f = 32;
	this.name = null == t ? void 0 : t.name;
	if (r) r.push(this);
}
E.prototype.c = function() {
	var i = this.S();
	try {
		if (8 & this.f) return;
		if (void 0 === this.x) return;
		var t = this.x();
		if ("function" == typeof t) this.m = t;
	} finally {
		i();
	}
};
E.prototype.S = function() {
	if (1 & this.f) throw new Error("Cycle detected");
	this.f |= 1;
	this.f &= -9;
	S(this);
	_$1(this);
	v++;
	var i = o;
	o = this;
	return x$1.bind(this, i);
};
E.prototype.N = function() {
	if (!(2 & this.f)) {
		this.f |= 2;
		this.u = h$1;
		h$1 = this;
	}
};
E.prototype.d = function() {
	this.f |= 8;
	if (!(1 & this.f)) m(this);
};
E.prototype.dispose = function() {
	this.d();
};
function j(i, t) {
	var n = new E(i, t);
	try {
		n.c();
	} catch (i) {
		n.d();
		throw i;
	}
	var r = n.d.bind(n);
	r[Symbol.dispose] = r;
	return r;
}
//#endregion
//#region node_modules/@preact/signals/dist/signals.module.js
var l;
var h;
var p = "undefined" != typeof window && !!window.__PREACT_SIGNALS_DEVTOOLS__;
var _ = [];
j(function() {
	l = this.N;
})();
function g(i, r) {
	l$3[i] = r.bind(null, l$3[i] || function() {});
}
function b(i) {
	if (h) {
		var n = h;
		h = void 0;
		n();
	}
	h = i && i.S();
}
function y(i) {
	var n = this, t = i.data, f = useSignal(t);
	f.name = "ReactiveDom";
	f.value = t;
	var e = T(function() {
		var i = n, t = n.__v;
		while (t = t.__) if (t.__c) {
			t.__c.__$f |= 4;
			break;
		}
		var o = g$1(function() {
			var i = f.value.value;
			return 0 === i ? 0 : !0 === i ? "" : i || "";
		}), e = g$1(function() {
			return !Array.isArray(o.value) && !t$3(o.value);
		}), a = j(function() {
			this.N = F;
			if (e.value) {
				var n = o.value;
				if (i.__v && i.__v.__e && 3 === i.__v.__e.nodeType) i.__v.__e.data = n;
			}
		}), v = n.__$u.d;
		n.__$u.d = function() {
			a();
			v.call(this);
		};
		return [e, o];
	}, []), a = e[0], v = e[1];
	return a.value ? v.peek() : v.value;
}
y.displayName = "ReactiveTextNode";
Object.defineProperties(l$1.prototype, {
	constructor: {
		configurable: !0,
		value: void 0
	},
	type: {
		configurable: !0,
		value: y
	},
	props: {
		configurable: !0,
		get: function() {
			var i = this;
			return { data: { get value() {
				return i.value;
			} } };
		}
	},
	__b: {
		configurable: !0,
		value: 1
	}
});
g("__b", function(i, n) {
	if ("string" == typeof n.type) {
		var r, t = n.props;
		for (var o in t) if ("children" !== o) {
			var f = t[o];
			if (f instanceof l$1) {
				if (!r) n.__np = r = {};
				r[o] = f;
				t[o] = f.peek();
			}
		}
	}
	i(n);
});
g("__r", function(i, n) {
	i(n);
	if (n.type !== S$1) {
		b();
		var r, o = n.__c;
		if (o) {
			o.__$f &= -2;
			if (void 0 === (r = o.__$u)) o.__$u = r = function(i, n) {
				var r;
				j(function() {
					r = this;
				}, { name: n });
				r.c = i;
				return r;
			}(function() {
				var i;
				if (p) null == (i = r.y) || i.call(r);
				o.__$f |= 1;
				o.setState({});
			}, "function" == typeof n.type ? n.type.displayName || n.type.name : "");
		}
		b(r);
	}
});
g("__e", function(i, n, r, t) {
	b();
	i(n, r, t);
});
g("diffed", function(i, n) {
	b();
	var r;
	if ("string" == typeof n.type && (r = n.__e)) {
		var t = n.__np, o = n.props, f = r.U;
		if (f) for (var e in f) {
			var u = f[e];
			if (!(void 0 === u || t && e in t)) {
				u.d();
				f[e] = void 0;
			}
		}
		if (t) {
			if (!f) {
				f = {};
				r.U = f;
			}
			for (var a in t) {
				var c = f[a], v = t[a];
				if (void 0 === c) {
					c = w(r, a, v, o);
					f[a] = c;
				} else c.o(v, o);
			}
		}
	}
	i(n);
});
function w(i, n, r, t) {
	var o = n in i && void 0 === i.ownerSVGElement, f = y$1(r);
	return {
		o: function(i, n) {
			f.value = i;
			t = n;
		},
		d: j(function() {
			this.N = F;
			var r = f.value.value;
			if (t[n] !== r) {
				t[n] = r;
				if (o) i[n] = r;
				else if (null != r && (!1 !== r || "-" === n[4])) i.setAttribute(n, r);
				else i.removeAttribute(n);
			}
		})
	};
}
g("unmount", function(i, n) {
	if ("string" == typeof n.type) {
		var r = n.__e;
		if (r) {
			var t = r.U;
			if (t) {
				r.U = void 0;
				for (var o in t) {
					var f = t[o];
					if (f) f.d();
				}
			}
		}
		var e = n.__np;
		if (e) {
			var u = n.props;
			for (var a in e) u[a] = e[a];
		}
		n.__np = void 0;
	} else {
		var c = n.__c;
		if (c) {
			var v = c.__$u;
			if (v) {
				c.__$u = void 0;
				v.d();
			}
		}
	}
	i(n);
});
g("__h", function(i, n, r, t) {
	if (t < 3) n.__$f |= 2;
	i(n, r, t);
});
C$1.prototype.shouldComponentUpdate = function(i, n) {
	if (this.__R) return !0;
	var r = this.__$u, t = r && void 0 !== r.s;
	for (var o in n) return !0;
	if (this.__f || "boolean" == typeof this.u && !0 === this.u) {
		var f = 2 & this.__$f;
		if (!(t || f || 4 & this.__$f)) return !0;
		if (1 & this.__$f) return !0;
	} else {
		if (!(t || 4 & this.__$f)) return !0;
		if (3 & this.__$f) return !0;
	}
	for (var e in i) if ("__source" !== e && i[e] !== this.props[e]) return !0;
	for (var u in this.props) if (!(u in i)) return !0;
	return !1;
};
function useSignal(i, n) {
	return T(function() {
		return y$1(i, n);
	}, []);
}
var q = function(i) {
	queueMicrotask(function() {
		queueMicrotask(i);
	});
};
function x() {
	n(function() {
		var i;
		while (i = _.shift()) l.call(i);
	});
}
function F() {
	if (1 === _.push(this)) (l$3.requestAnimationFrame || q)(x);
}
//#endregion
//#region src/contracts.ts
var emptySnapshot = {
	state: "unknown",
	http_reachable: false,
	websocket_online: false,
	device_url: "",
	reconnect_attempt: 0
};
function isSnapshot(value) {
	if (!value || typeof value !== "object") return false;
	const candidate = value;
	return (candidate.state === "on" || candidate.state === "off" || candidate.state === "unknown") && typeof candidate.http_reachable === "boolean" && typeof candidate.websocket_online === "boolean" && typeof candidate.device_url === "string";
}
function isAppConfig(value) {
	if (!value || typeof value !== "object") return false;
	const candidate = value;
	return Boolean(candidate.lamp && candidate.ui && candidate.notifications && candidate.hotkeys && candidate.automation && candidate.integration && candidate.diagnostics);
}
//#endregion
//#region src/theme.ts
var webThemeStorageKey = "rayanlamp.web.theme";
var lightThemeColor = "#eef4f8";
var darkThemeColor = "#0b1724";
var systemThemeQuery = "(prefers-color-scheme: light)";
var webTheme = y$1(readStoredTheme());
var resolvedWebTheme = y$1(resolveWebTheme(webTheme.value, systemPrefersLight()));
var activeMediaQuery = null;
var detachMediaListener = null;
function readStoredTheme() {
	try {
		const value = localStorage.getItem(webThemeStorageKey);
		if (isWebTheme(value)) return value;
	} catch {}
	return "system";
}
function applyWebTheme(theme, persist = true) {
	webTheme.value = theme;
	if (persist) try {
		localStorage.setItem(webThemeStorageKey, theme);
	} catch {}
	return reconcileTheme();
}
function startWebThemeController() {
	detachMediaListener?.();
	detachMediaListener = null;
	activeMediaQuery = typeof window.matchMedia === "function" ? window.matchMedia(systemThemeQuery) : null;
	const media = activeMediaQuery;
	let detachThisController = null;
	if (media) {
		const onChange = () => {
			if (webTheme.value === "system") reconcileTheme();
		};
		media.addEventListener("change", onChange);
		detachThisController = () => media.removeEventListener("change", onChange);
		detachMediaListener = detachThisController;
	}
	reconcileTheme();
	let stopped = false;
	return () => {
		if (stopped) return;
		stopped = true;
		detachThisController?.();
		if (detachMediaListener === detachThisController) {
			detachMediaListener = null;
			activeMediaQuery = null;
		}
	};
}
function resolveWebTheme(requested, prefersLight) {
	if (requested === "light") return "light";
	if (requested === "dark") return "dark";
	return prefersLight ? "light" : "dark";
}
function reconcileTheme() {
	const resolved = resolveWebTheme(webTheme.value, activeMediaQuery?.matches ?? systemPrefersLight());
	resolvedWebTheme.value = resolved;
	const root = document.documentElement;
	root.dataset.theme = webTheme.value;
	root.dataset.resolvedTheme = resolved;
	root.style.colorScheme = resolved;
	const colorScheme = document.querySelector("meta[name=\"color-scheme\"]");
	if (colorScheme) colorScheme.content = resolved;
	const themeColor = document.querySelector("meta[name=\"theme-color\"]");
	if (themeColor) themeColor.content = resolved === "light" ? lightThemeColor : darkThemeColor;
	return resolved;
}
function systemPrefersLight() {
	return typeof window.matchMedia === "function" && window.matchMedia(systemThemeQuery).matches;
}
function isWebTheme(value) {
	return value === "system" || value === "light" || value === "dark";
}
//#endregion
//#region node_modules/preact/jsx-runtime/dist/jsxRuntime.module.js
var f = 0;
Array.isArray;
function u(e, t, n, o, i, u) {
	t || (t = {});
	var a, c, p = t;
	if ("ref" in p) for (c in p = {}, t) "ref" == c ? a = t[c] : p[c] = t[c];
	var l = {
		type: e,
		props: p,
		key: n,
		ref: a,
		__k: null,
		__: null,
		__b: 0,
		__e: null,
		__c: null,
		constructor: void 0,
		__v: --f,
		__i: -1,
		__u: 0,
		__source: i,
		__self: u
	};
	if ("function" == typeof e && (a = e.defaultProps)) for (c in a) void 0 === p[c] && (p[c] = a[c]);
	return l$3.vnode && l$3.vnode(l), l;
}
//#endregion
//#region src/i18n.tsx
var STORAGE_KEY = "rayanlamp.web.locale";
var localePreference = y$1("auto");
var resolvedLocale = y$1("en");
var fa = {
	"Rayan Lamp": "چراغ رایان",
	"Dashboard": "داشبورد",
	"Settings": "تنظیمات",
	"Terminal": "پایانه",
	"Diagnostics": "عیب‌یابی",
	"About": "درباره",
	"Local controller": "کنترل‌گر محلی",
	"Private local control surface for Rayan Lamp": "رابط کنترل خصوصی و محلی چراغ رایان",
	"Skip to content": "رفتن به محتوای اصلی",
	"Open dashboard": "باز کردن داشبورد",
	"Primary navigation": "پیمایش اصلی",
	"Connection": "اتصال",
	"Connection: {name}": "اتصال: {name}",
	"Choose how this dashboard connects": "روش اتصال این داشبورد را انتخاب کنید",
	"Color theme": "پوستهٔ رنگی",
	"System": "سیستم",
	"Light": "روشن",
	"Dark": "تیره",
	"{theme} theme": "پوستهٔ {theme}",
	"HTTP polling": "پایش HTTP",
	"Events connected": "رویدادها متصل‌اند",
	"Reconnecting": "در حال اتصال دوباره",
	"WebSocket events disabled; HTTP polling is active": "رویدادهای WebSocket غیرفعال‌اند؛ پایش HTTP فعال است",
	"Realtime events connected": "رویدادهای زنده متصل‌اند",
	"Realtime events reconnecting": "رویدادهای زنده در حال اتصال دوباره‌اند",
	"Dashboard update ready": "به‌روزرسانی داشبورد آماده است",
	"A new bundled web interface is available from the running RayanLamp executable.": "نسخهٔ تازه‌ای از رابط وب همراه برنامهٔ در حال اجرای چراغ رایان آماده است.",
	"Later": "بعداً",
	"Reload dashboard": "بارگذاری دوبارهٔ داشبورد",
	"Connecting to Rayan Lamp": "در حال اتصال به چراغ رایان",
	"Selecting a transport and synchronizing confirmed lamp state…": "در حال انتخاب مسیر ارتباطی و همگام‌سازی وضعیت تأییدشدهٔ چراغ…",
	"Selected connection unavailable": "اتصال انتخاب‌شده در دسترس نیست",
	"The dashboard could not reach its controller": "داشبورد نتوانست به کنترل‌گر متصل شود",
	"Coordinator mode needs the RayanLamp Go service. Direct mode needs this page served over HTTP and browser access to the lamp origin.": "حالت هماهنگ‌کننده به سرویس Go چراغ رایان نیاز دارد. در حالت مستقیم، این صفحه باید با HTTP ارائه شود و مرورگر به مبدأ چراغ دسترسی داشته باشد.",
	"Connection settings": "تنظیمات اتصال",
	"Try again": "تلاش دوباره",
	"Offline fallback ready": "نسخهٔ آفلاین آماده است",
	"The app shell is cached; controls reconnect through the selected local transport.": "پوستهٔ برنامه در حافظه ذخیره شد؛ کنترل‌ها از مسیر محلی انتخاب‌شده دوباره متصل می‌شوند.",
	"The local Rayan Lamp controller is unavailable.": "کنترل‌گر محلی چراغ رایان در دسترس نیست.",
	"Selecting connection": "در حال انتخاب اتصال",
	"Lamp unavailable": "چراغ در دسترس نیست",
	"Lamp is offline": "چراغ آفلاین است",
	"Lamp state is unknown": "وضعیت چراغ نامشخص است",
	"Lamp is on": "چراغ روشن است",
	"Lamp is off": "چراغ خاموش است",
	"Checking lamp": "در حال بررسی چراغ",
	"Office lighting": "روشنایی دفتر",
	"Local, private control with a resilient live connection.": "کنترل محلی و خصوصی با اتصال زنده و پایدار.",
	"Configured lamp": "چراغ پیکربندی‌شده",
	"Turn lamp off": "خاموش کردن چراغ",
	"Turn lamp on": "روشن کردن چراغ",
	"Lamp controls": "کنترل‌های چراغ",
	"Toggle lamp": "تغییر وضعیت چراغ",
	"Turn on": "روشن کن",
	"Turn off": "خاموش کن",
	"Device channels": "مسیرهای ارتباطی دستگاه",
	"HTTP control": "کنترل HTTP",
	"WebSocket events": "رویدادهای WebSocket",
	"Available": "در دسترس",
	"Unavailable": "در دسترس نیست",
	"Connected": "متصل",
	"Disconnected": "قطع",
	"Last source": "آخرین منبع",
	"Waiting for an event": "در انتظار رویداد",
	"Last change": "آخرین تغییر",
	"Firmware": "میان‌افزار",
	"Wi-Fi signal": "قدرت Wi-Fi",
	"Lamp state refreshed": "وضعیت چراغ تازه شد",
	"Refresh": "تازه‌سازی",
	"Reconnect": "اتصال دوباره",
	"Desktop app": "برنامهٔ دسکتاپ",
	"Native window": "پنجرهٔ بومی",
	"Bring the compact controller forward or tuck it back into the notification area.": "کنترل‌گر فشرده را به پیش‌زمینه بیاورید یا در ناحیهٔ اعلان‌ها پنهان کنید.",
	"Native window opened": "پنجرهٔ بومی باز شد",
	"Native window hidden": "پنجرهٔ بومی پنهان شد",
	"Show": "نمایش",
	"Hide": "پنهان کردن",
	"Reconnect to Rayan Lamp?": "اتصال به چراغ رایان دوباره برقرار شود؟",
	"The current HTTP and WebSocket transports will restart. The cached .local address is retained unless the connection fails.": "مسیرهای HTTP و WebSocket فعلی راه‌اندازی دوباره می‌شوند. نشانی ذخیره‌شدهٔ .local تا زمانی که اتصال شکست نخورد حفظ می‌شود.",
	"Keep connection": "حفظ اتصال",
	"Reconnection started": "اتصال دوباره آغاز شد",
	"Partially connected": "اتصال محدود",
	"Starting": "در حال راه‌اندازی",
	"Applying {command}…": "در حال اجرای {command}…",
	"Press the lamp or use an explicit action below.": "چراغ را فشار دهید یا یکی از فرمان‌های زیر را انتخاب کنید.",
	"Not reported yet": "هنوز گزارش نشده",
	"Command failed": "فرمان ناموفق بود",
	"The lamp did not respond.": "چراغ پاسخ نداد.",
	"Connection mode": "حالت اتصال",
	"Choose who owns lamp communication. This preference is shared with other dashboard tabs.": "مسئول ارتباط با چراغ را انتخاب کنید. این ترجیح میان زبانه‌های داشبورد همگام است.",
	"Active now": "اکنون فعال",
	"Preferred transport": "مسیر ارتباطی ترجیحی",
	"Automatic — coordinator, then direct": "خودکار — ابتدا هماهنگ‌کننده، سپس مستقیم",
	"Go coordinator": "هماهنگ‌کنندهٔ Go",
	"Direct to lamp": "مستقیم به چراغ",
	"Automatic connection": "اتصال خودکار",
	"Headless Go coordinator": "هماهنگ‌کنندهٔ Go بدون رابط",
	"Uses the same-origin RayanLamp manager for resilient discovery, settings, diagnostics, automation, and native integration.": "از مدیر هم‌مبدأ چراغ رایان برای کشف پایدار، تنظیمات، عیب‌یابی، خودکارسازی و یکپارچگی بومی استفاده می‌کند.",
	"Direct browser connection": "اتصال مستقیم مرورگر",
	"Uses firmware HTTP and WebSocket endpoints. Native-only features are intentionally hidden.": "از پایانه‌های HTTP و WebSocket میان‌افزار استفاده می‌کند. امکانات صرفاً بومی عمداً پنهان‌اند.",
	"Lamp address": "نشانی چراغ",
	"HTTP or HTTPS origin only; no path, query, fragment, or credentials.": "فقط مبدأ HTTP یا HTTPS؛ بدون مسیر، پرس‌وجو، قطعه یا اطلاعات ورود.",
	"The static edition cannot borrow the manager’s private browser session cross-origin. Open the dashboard served by RayanLamp.exe to use coordinator mode.": "نسخهٔ ایستا نمی‌تواند نشست خصوصی مرورگر مدیر را میان مبدأها به‌کار بگیرد. برای حالت هماهنگ‌کننده، داشبورد ارائه‌شده توسط RayanLamp.exe را باز کنید.",
	"Serve the ZIP over HTTP. An HTTPS page cannot control the current HTTP/WS lamp because browsers block mixed content.": "فایل ZIP را با HTTP ارائه کنید. صفحهٔ HTTPS نمی‌تواند چراغ HTTP/WS فعلی را کنترل کند، زیرا مرورگرها محتوای ترکیبی را مسدود می‌کنند.",
	"Cancel": "لغو",
	"Apply and reconnect": "اعمال و اتصال دوباره",
	"Enter a valid lamp address.": "نشانی معتبر چراغ را وارد کنید.",
	"Close dialog": "بستن پنجره",
	"Dismiss {title}": "بستن اعلان «{title}»",
	"Application": "برنامه",
	"About Rayan Lamp": "دربارهٔ چراغ رایان",
	"A private, single-executable Windows controller by YektaYar.": "کنترل‌گر خصوصی و تک‌فایلی ویندوز، ساختهٔ یکتایار.",
	"Version": "نسخه",
	"Edition": "ویرایش",
	"Development": "توسعه",
	"Production": "نهایی",
	"Commit": "کامیت",
	"Built": "زمان ساخت",
	"Local by design": "محلی از پایه",
	"Direct mode sends simple requests from this browser to the configured lamp. No cloud service or manager token is involved.": "حالت مستقیم درخواست‌های ساده را از همین مرورگر به چراغ پیکربندی‌شده می‌فرستد؛ هیچ سرویس ابری یا توکن مدیری درگیر نیست.",
	"The Go coordinator serves this dashboard on a loopback-only listener. Its temporary browser session never exposes the long-lived API bearer token.": "هماهنگ‌کنندهٔ Go این داشبورد را تنها روی نشانی loopback ارائه می‌کند. نشست موقت مرورگر هرگز توکن بلندمدت API را آشکار نمی‌کند.",
	"Shared state": "وضعیت مشترک",
	"Tabs share one elected firmware event socket and fan confirmed lamp state through BroadcastChannel.": "زبانه‌ها یک سوکت رویداد منتخب میان‌افزار را مشترکاً استفاده می‌کنند و وضعیت تأییدشدهٔ چراغ را با BroadcastChannel پخش می‌کنند.",
	"The headless web host, CLI, terminal UI, IPC clients, and automation share one command dispatcher and confirmed lamp snapshot.": "میزبان وب بدون رابط، CLI، رابط ترمینالی، کاربران IPC و خودکارسازی از یک توزیع‌کنندهٔ فرمان و تصویر تأییدشدهٔ چراغ بهره می‌برند.",
	"Native UI, tray, CLI, terminal UI, automation, and this web client all reach the same command dispatcher and confirmed lamp snapshot.": "رابط بومی، سینی، CLI، رابط ترمینالی، خودکارسازی و این کارخواه وب همگی به یک توزیع‌کنندهٔ فرمان و تصویر تأییدشدهٔ چراغ متصل‌اند.",
	"Web session": "نشست وب",
	"Events reconnecting": "رویدادها در حال اتصال دوباره‌اند",
	"Active transport:": "مسیر فعال:",
	"Coordinator-secured TCP bridge": "پل TCP ایمن‌شده با هماهنگ‌کننده",
	"Device terminal": "پایانهٔ دستگاه",
	"An ANSI terminal connected to the lamp through the local Go coordinator. Output exists only in this page’s memory.": "پایانه‌ای ANSI که از راه هماهنگ‌کنندهٔ محلی Go به چراغ متصل است. خروجی فقط در حافظهٔ همین صفحه نگه‌داری می‌شود.",
	"Firmware terminal is read-only": "پایانهٔ میان‌افزار فقط‌خواندنی است",
	"This firmware reports that its interactive shell is assigned to Serial. Network output remains available, but keyboard input is intentionally disabled.": "میان‌افزار اعلام کرده پوستهٔ تعاملی به Serial اختصاص دارد. خروجی شبکه همچنان در دسترس است، اما ورودی صفحه‌کلید عمداً غیرفعال شده.",
	"Terminal needs attention": "پایانه نیاز به توجه دارد",
	"Rayan Lamp device terminal input": "ورودی پایانهٔ دستگاه چراغ رایان",
	"The terminal renderer could not be loaded.": "رندرکنندهٔ پایانه بارگذاری نشد.",
	"The coordinator session could not be renewed.": "نشست هماهنگ‌کننده تمدید نشد.",
	"Reconnect coordinator": "اتصال دوبارهٔ هماهنگ‌کننده",
	"Preparing": "در حال آماده‌سازی",
	"Connect": "اتصال",
	"Disconnect": "قطع اتصال",
	"Clear": "پاک کردن",
	"Rayan Lamp terminal output": "خروجی پایانهٔ چراغ رایان",
	"Opening the device terminal…": "در حال باز کردن پایانهٔ دستگاه…",
	"Connect when you need the device console": "در زمان نیاز به کنسول دستگاه متصل شوید",
	"Send one short command at a time; firmware lines are limited to 64 ASCII bytes.": "هر بار یک فرمان کوتاه بفرستید؛ طول خطوط میان‌افزار به ۶۴ بایت ASCII محدود است.",
	"Output-only session": "نشست فقط‌خروجی",
	"No input is sent while disconnected.": "در حالت قطع، هیچ ورودی فرستاده نمی‌شود.",
	"Session {session} · Sequence {sequence}": "نشست {session} · دنباله {sequence}",
	"Device-shell commands can change firmware settings or restart the lamp. The coordinator fixes the destination to the configured lamp and terminal port; this browser never opens raw TCP itself.": "فرمان‌های پوستهٔ دستگاه می‌توانند تنظیمات میان‌افزار را تغییر دهند یا چراغ را راه‌اندازی دوباره کنند. هماهنگ‌کننده مقصد را به چراغ و درگاه پایانهٔ پیکربندی‌شده محدود می‌کند؛ این مرورگر هرگز مستقیماً TCP خام باز نمی‌کند.",
	"Device console": "کنسول دستگاه",
	"The interactive terminal is intentionally isolated behind the Go coordinator.": "پایانهٔ تعاملی عمداً پشت هماهنگ‌کنندهٔ Go ایزوله شده است.",
	"Coordinator required": "هماهنگ‌کننده لازم است",
	"Raw TCP is not available in direct browser mode": "TCP خام در حالت مستقیم مرورگر در دسترس نیست",
	"Browsers cannot connect directly to the lamp’s Telnet port. Choose Automatic or Go coordinator to use the authenticated same-origin terminal bridge.": "مرورگرها نمی‌توانند مستقیماً به درگاه Telnet چراغ متصل شوند. برای استفاده از پل پایانهٔ هم‌مبدأ و احراز‌شده، «خودکار» یا «هماهنگ‌کنندهٔ Go» را انتخاب کنید.",
	"For the frontend-only ZIP, serve it behind the same origin that forwards /app/session and the versionless API paths to a running RayanLamp coordinator.": "برای ZIP فقط‌فرانت‌اند، آن را پشت همان مبدأیی ارائه کنید که ‎/app/session و مسیرهای بدون نسخهٔ API را به هماهنگ‌کنندهٔ در حال اجرای چراغ رایان هدایت می‌کند.",
	"For the frontend-only ZIP, serve it behind the same origin that forwards": "برای ZIP فقط‌فرانت‌اند، آن را پشت همان مبدأیی ارائه کنید که",
	"and the versionless API paths to a running RayanLamp coordinator.": "و مسیرهای بدون نسخهٔ API را به هماهنگ‌کنندهٔ در حال اجرای چراغ رایان هدایت می‌کند.",
	"Preparing bridge": "در حال آماده‌سازی پل",
	"Coordinator unavailable": "هماهنگ‌کننده در دسترس نیست",
	"Terminal connected": "پایانه متصل است",
	"Connecting to device": "در حال اتصال به دستگاه",
	"Terminal disconnected": "پایانه قطع است",
	"Language": "زبان",
	"Follow system language": "پیروی از زبان سیستم",
	"English": "انگلیسی",
	"Persian": "فارسی",
	"Interface language": "زبان رابط",
	"Changes direction, typography, digits, and reader-facing dates immediately.": "جهت، تایپوگرافی، رقم‌ها و تاریخ‌های نمایشی را بی‌درنگ تغییر می‌دهد.",
	"Automatic": "خودکار",
	"English (US)": "انگلیسی",
	"فارسی": "فارسی",
	"Lamp": "چراغ",
	"Appearance": "ظاهر",
	"Notifications": "اعلان‌ها",
	"Hotkeys": "کلیدهای میان‌بر",
	"Automation": "خودکارسازی",
	"Integration": "یکپارچگی",
	"Address and transport": "نشانی و مسیر ارتباطی",
	"Native window and material": "پنجرهٔ بومی و جلوه‌ها",
	"Toasts, balloons, and sound": "اعلان، بالون و صدا",
	"System-wide shortcuts": "میان‌برهای سراسری",
	"State-change scripts": "اسکریپت‌های تغییر وضعیت",
	"Startup and local API": "راه‌اندازی و API محلی",
	"Endpoint allowlists": "فهرست مجاز پایانه‌ها",
	"Rayan Lamp notifications are ready.": "اعلان‌های چراغ رایان آماده‌اند.",
	"Loading settings…": "در حال بارگذاری تنظیمات…",
	"Unable to save settings.": "تنظیمات ذخیره نشد.",
	"Settings were not saved": "تنظیمات ذخیره نشدند",
	"Configuration": "پیکربندی",
	"Direct connection preferences are stored in this browser and synchronized across tabs.": "ترجیحات اتصال مستقیم در این مرورگر ذخیره و میان زبانه‌ها همگام می‌شوند.",
	"Changes apply automatically and reconcile with the native app.": "تغییرات خودکار اعمال و با برنامهٔ بومی همگام می‌شوند.",
	"Saving…": "در حال ذخیره…",
	"Needs attention": "نیازمند توجه",
	"Up to date": "به‌روز",
	"Latest change was not accepted.": "آخرین تغییر پذیرفته نشد.",
	"Retry": "تلاش دوباره",
	"Settings categories": "دسته‌های تنظیمات",
	"Turn off the local API?": "API محلی خاموش شود؟",
	"The bundled web dashboard uses this same private loopback listener. This page will disconnect immediately.": "داشبورد وب همراه برنامه از همین شنوندهٔ خصوصی loopback استفاده می‌کند. اتصال این صفحه فوراً قطع می‌شود.",
	"Keep enabled": "روشن بماند",
	"Disable and disconnect": "غیرفعال‌سازی و قطع اتصال",
	"Test a notification": "آزمایش اعلان",
	"Visual delivery and sound remain independent and follow the settings above.": "نمایش اعلان و پخش صدا مستقل‌اند و از تنظیمات بالا پیروی می‌کنند.",
	"Message": "پیام",
	"Rayan Lamp test": "آزمایش چراغ رایان",
	"Test notification sent": "اعلان آزمایشی فرستاده شد",
	"Notification test failed": "آزمایش اعلان ناموفق بود",
	"Send test": "ارسال آزمایش",
	"Lamp connection": "اتصال چراغ",
	"This browser talks to the lamp over CORS-enabled HTTP and its firmware WebSocket.": "این مرورگر از راه HTTP با CORS فعال و WebSocket میان‌افزار با چراغ ارتباط برقرار می‌کند.",
	"The native controller owns discovery, HTTP commands, and the resilient event socket.": "کنترل‌گر بومی مسئول کشف دستگاه، فرمان‌های HTTP و سوکت پایدار رویدادهاست.",
	"Device": "دستگاه",
	"HTTP or HTTPS origin without a path, query, or embedded credentials.": "مبدأ HTTP یا HTTPS بدون مسیر، پرس‌وجو یا اطلاعات ورود توکار.",
	"Request timeout": "مهلت درخواست",
	"Duration from 100ms through 2m.": "مدت‌زمان از ۱۰۰ میلی‌ثانیه تا ۲ دقیقه.",
	"Go duration from 100ms through 5m.": "مدت‌زمان Go از ۱۰۰ میلی‌ثانیه تا ۵ دقیقه.",
	"Device terminal port": "درگاه پایانهٔ دستگاه",
	"Retained for configuration compatibility; direct browsers cannot open this raw TCP port.": "برای سازگاری پیکربندی نگه داشته می‌شود؛ مرورگر مستقیم نمی‌تواند این درگاه TCP خام را باز کند.",
	"Audited Telnet-style device console port. The default firmware uses TCP 23.": "درگاه بررسی‌شدهٔ کنسول دستگاه به سبک Telnet. میان‌افزار پیش‌فرض از TCP 23 استفاده می‌کند.",
	"Live updates": "به‌روزرسانی زنده",
	"Listen for WebSocket events": "دریافت رویدادهای WebSocket",
	"Connect to /ws, request one state, then passively receive updates.": "به ‎/ws متصل می‌شود، یک وضعیت می‌گیرد و سپس به‌روزرسانی‌ها را دریافت می‌کند.",
	"Reconnect minimum": "کمینهٔ اتصال دوباره",
	"Reconnect maximum": "بیشینهٔ اتصال دوباره",
	"HTTP polling interval": "فاصلهٔ پایش HTTP",
	"Use 0s to disable fallback polling.": "برای غیرفعال کردن پایش جایگزین، 0s وارد کنید.",
	"Native app appearance": "ظاهر برنامهٔ بومی",
	"Web interface appearance": "ظاهر رابط وب",
	"Language and theme are shared with this dashboard; the remaining controls affect the Windows window.": "زبان و پوسته با این داشبورد مشترک‌اند؛ سایر کنترل‌ها بر پنجرهٔ ویندوز اثر می‌گذارند.",
	"Language, direction, typography, theme, and motion apply to this browser immediately.": "زبان، جهت، تایپوگرافی، پوسته و حرکت بی‌درنگ بر این مرورگر اعمال می‌شوند.",
	"Dashboard appearance": "ظاهر داشبورد",
	"These controls affect the Windows window, not this browser dashboard.": "این کنترل‌ها بر پنجرهٔ ویندوز اثر دارند، نه داشبورد مرورگر.",
	"Layout and material": "چیدمان و جلوه",
	"Window layout": "چیدمان پنجره",
	"Compact lamp": "چراغ فشرده",
	"Larger dashboard": "داشبورد بزرگ‌تر",
	"Use Windows setting": "استفاده از تنظیم ویندوز",
	"Backdrop": "پس‌زمینه",
	"Solid (no effect)": "یکدست (بدون جلوه)",
	"Transparent": "شفاف",
	"Blur behind": "تاری پشت پنجره",
	"Acrylic": "اکریلیک",
	"Mica": "میکا",
	"Tabbed": "زبانه‌ای",
	"Rendering backend": "موتور رندر",
	"Automatic uses DirectX 11 hardware composition when supported. Choose Compatibility for lower RAM while retaining the same UI features.": "حالت خودکار در صورت پشتیبانی از ترکیب سخت‌افزاری DirectX 11 استفاده می‌کند. برای RAM کمتر و حفظ همان امکانات رابط، حالت سازگار را انتخاب کنید.",
	"Automatic (DirectX when supported)": "خودکار (DirectX در صورت پشتیبانی)",
	"DirectX 11 hardware": "سخت‌افزار DirectX 11",
	"Compatibility renderer (lower RAM)": "رندرکنندهٔ سازگار (RAM کمتر)",
	"Title bar": "نوار عنوان",
	"None": "هیچ‌کدام",
	"Custom controls": "کنترل‌های سفارشی",
	"Native Windows title bar": "نوار عنوان بومی ویندوز",
	"Window border": "حاشیهٔ پنجره",
	"Automatic keeps edge resizing. None removes the outer frame.": "حالت خودکار امکان تغییر اندازه از لبه را حفظ می‌کند؛ «هیچ‌کدام» قاب بیرونی را حذف می‌کند.",
	"Automatic (resizable)": "خودکار (قابل تغییر اندازه)",
	"None (frameless)": "بدون حاشیه",
	"Rounded corners": "گوشه‌های گرد",
	"Layered Fluent foreground": "پیش‌زمینهٔ لایه‌ای Fluent",
	"Sharp SVG artwork, alpha-composited controls, reveal lighting, and motion.": "طرح SVG شفاف، کنترل‌های ترکیب‌شده با آلفا، نورپردازی Reveal و حرکت.",
	"Animations": "پویانمایی‌ها",
	"Window behavior": "رفتار پنجره",
	"Always on top": "همیشه روی سایر پنجره‌ها",
	"Close to notification area": "بستن به ناحیهٔ اعلان‌ها",
	"Preload and reuse the notification-area menu": "پیش‌بارگذاری و استفادهٔ دوباره از منوی ناحیهٔ اعلان",
	"Prepares the menu invisibly and reuses its window between openings to avoid first-frame flashes. Disable to recreate it each time.": "منو را پنهانی آماده می‌کند و پنجره‌اش را میان دفعات باز شدن نگه می‌دارد تا فلاش فریم نخست رخ ندهد. برای ساخت دوباره در هر بار، غیرفعال کنید.",
	"Start minimized": "شروع در حالت کمینه",
	"Show taskbar button": "نمایش دکمه در نوار وظیفه",
	"Magnetic snap": "چسبندگی مغناطیسی",
	"Hold Ctrl while dragging to bypass snapping; release Ctrl to resume it immediately.": "هنگام کشیدن Ctrl را نگه دارید تا چسبندگی موقتاً نادیده گرفته شود؛ با رها کردن Ctrl فوراً از سر گرفته می‌شود.",
	"Snap distance": "فاصلهٔ چسبندگی",
	"Device-independent pixels; 1 through 128.": "پیکسل مستقل از دستگاه؛ از ۱ تا ۱۲۸.",
	"Stored window bounds": "ابعاد ذخیره‌شدهٔ پنجره",
	"Use -1 for X or Y to request default centering.": "برای مرکزچینی پیش‌فرض، مقدار X یا Y را ‎-1 بگذارید.",
	"Notifications and sound": "اعلان‌ها و صدا",
	"Visual delivery and audible cues are independent.": "نمایش اعلان و نشانه‌های صوتی مستقل‌اند.",
	"Audible cues remain available without native visual notifications.": "نشانه‌های صوتی بدون اعلان‌های تصویری بومی نیز در دسترس‌اند.",
	"Visual delivery": "نمایش اعلان",
	"Enable notifications": "فعال‌سازی اعلان‌ها",
	"Toast, then balloon fallback": "Toast، سپس Balloon جایگزین",
	"Modern toast only": "فقط Toast مدرن",
	"Legacy tray balloon": "Balloon قدیمی سینی",
	"Confirmed state changes": "تغییرات تأییدشدهٔ وضعیت",
	"Firmware messages": "پیام‌های میان‌افزار",
	"Verify delivery": "بررسی اعلان",
	"Send a local test without changing any lamp state.": "یک آزمایش محلی بدون تغییر وضعیت چراغ بفرستید.",
	"Test notification": "آزمایش اعلان",
	"Event timing": "زمان‌بندی رویداد",
	"Initial synchronization": "همگام‌سازی نخستین",
	"Include the first known lamp state after connecting.": "نخستین وضعیت شناخته‌شدهٔ چراغ پس از اتصال نیز شامل شود.",
	"Audio effects": "جلوه‌های صوتی",
	"Direct mode plays in the elected browser tab only, so synchronized tabs never echo.": "حالت مستقیم فقط در زبانهٔ منتخب مرورگر پخش می‌کند تا زبانه‌های همگام صدا را تکرار نکنند.",
	"The coordinator owns playback, including when this dashboard is opened remotely.": "هماهنگ‌کننده مسئول پخش است، حتی هنگامی که داشبورد از راه دور باز شده باشد.",
	"Audible cues": "نشانه‌های صوتی",
	"State and message sounds work even when visual notifications are disabled.": "صدای وضعیت و پیام حتی با اعلان تصویری غیرفعال نیز پخش می‌شود.",
	"Sound source": "منبع صدا",
	"Bundled Rayan Lamp effects": "جلوه‌های همراه چراغ رایان",
	"Custom PCM WAV": "فایل PCM WAV سفارشی",
	"Choose custom WAV": "انتخاب WAV سفارشی",
	"Content-addressed custom WAV": "WAV سفارشی با شناسهٔ محتوا",
	"No custom sound imported": "صدای سفارشی وارد نشده",
	"PCM WAV · mono or stereo · up to 4 MiB": "PCM WAV · تک‌کاناله یا استریو · تا ۴ مگابایت",
	"Replace": "جایگزینی",
	"Import": "وارد کردن",
	"Verify audio": "بررسی صدا",
	"Play the selected cue in this browser.": "نشانهٔ انتخاب‌شده را در این مرورگر پخش کنید.",
	"Ask the coordinator to play the selected cue once.": "از هماهنگ‌کننده بخواهید نشانهٔ انتخاب‌شده را یک بار پخش کند.",
	"Test audio": "آزمایش صدا",
	"Custom sound imported": "صدای سفارشی وارد شد",
	"Audio import failed": "وارد کردن صدا ناموفق بود",
	"Audio played in this browser": "صدا در این مرورگر پخش شد",
	"Audio test sent to Rayan Lamp": "آزمایش صدا به چراغ رایان فرستاده شد",
	"Audio test failed": "آزمایش صدا ناموفق بود",
	"Global hotkeys": "کلیدهای میان‌بر سراسری",
	"Use modifier+key chords. Leave a field blank to disable it.": "از ترکیب کلید اصلاح‌گر و کلید اصلی استفاده کنید. برای غیرفعال‌سازی، فیلد را خالی بگذارید.",
	"Bindings": "اتصال کلیدها",
	"Record a chord directly. Completed shortcuts are normalized and synchronized immediately.": "ترکیب را مستقیماً ضبط کنید. میان‌بر کامل فوراً استاندارد و همگام می‌شود.",
	"Show window": "نمایش پنجره",
	"Shortcuts are checked with Windows before they are applied. Windows-key and other system-reserved combinations may still be handled before a browser sees them.": "میان‌برها پیش از اعمال در ویندوز بررسی می‌شوند. ترکیب‌های دارای کلید Windows و سایر ترکیب‌های رزروشده ممکن است پیش از رسیدن به مرورگر مدیریت شوند.",
	"This controller can safely pause global shortcuts while you record, but cannot preflight availability. Windows-key and system-reserved combinations are best recorded in native Settings.": "کنترل‌گر هنگام ضبط، میان‌برهای سراسری را ایمن متوقف می‌کند، اما نمی‌تواند در دسترس بودن را از پیش آزمایش کند. ترکیب‌های دارای کلید Windows و رزروشده بهتر است در تنظیمات بومی ضبط شوند.",
	"Browser-only recording cannot preflight Windows-wide availability. Windows-key and system-reserved combinations are best recorded in native Settings.": "ضبط مرورگری نمی‌تواند در دسترس بودن سراسری در ویندوز را از پیش آزمایش کند. ترکیب‌های دارای کلید Windows و رزروشده بهتر است در تنظیمات بومی ضبط شوند.",
	"Global shortcut": "میان‌بر سراسری",
	"This stored shortcut is not supported. Record a replacement or clear it.": "میان‌بر ذخیره‌شده پشتیبانی نمی‌شود؛ جایگزینی ضبط کنید یا آن را پاک کنید.",
	"F12 is reserved by Windows. Record a different shortcut or clear it.": "کلید F12 در ویندوز رزرو شده است؛ میان‌بر دیگری ضبط یا این مورد را پاک کنید.",
	"Preparing secure shortcut capture…": "در حال آماده‌سازی ضبط ایمن میان‌بر…",
	"Checking whether Windows can register this shortcut…": "در حال بررسی امکان ثبت میان‌بر در ویندوز…",
	"Applying the shortcut before restoring global hotkeys…": "در حال اعمال میان‌بر پیش از بازگرداندن میان‌برهای سراسری…",
	"Holding {keys}. Press the main key; Escape cancels.": "{keys} نگه داشته شده؛ کلید اصلی را فشار دهید. Escape لغو می‌کند.",
	"Waiting for a shortcut. Escape cancels; Backspace or Delete clears.": "در انتظار میان‌بر؛ Escape لغو و Backspace یا Delete پاک می‌کند.",
	"The stored shortcut needs attention.": "میان‌بر ذخیره‌شده نیازمند توجه است.",
	"{keys} is stored but unavailable.": "{keys} ذخیره شده، اما در دسترس نیست.",
	"{keys} is assigned.": "{keys} اختصاص یافته است.",
	"No shortcut is assigned.": "میان‌بری اختصاص نیافته است.",
	"Press your shortcut…": "میان‌بر را فشار دهید…",
	"Not assigned": "اختصاص نیافته",
	"Preparing {label} shortcut recording": "در حال آماده‌سازی ضبط میان‌بر {label}",
	"Checking {label} shortcut availability": "در حال بررسی دسترسی میان‌بر {label}",
	"Applying {label} shortcut": "در حال اعمال میان‌بر {label}",
	"Cancel recording for {label}": "لغو ضبط برای {label}",
	"Record {label} shortcut": "ضبط میان‌بر {label}",
	"Checking": "در حال بررسی",
	"Applying": "در حال اعمال",
	"Record": "ضبط",
	"Clear {label} shortcut": "پاک کردن میان‌بر {label}",
	"Already assigned to {label}. Choose another shortcut.": "پیش‌تر به {label} اختصاص یافته؛ میان‌بر دیگری انتخاب کنید.",
	"F12 is reserved by Windows.": "کلید F12 در ویندوز رزرو شده است.",
	"System-wide single-key shortcut — use carefully.": "میان‌بر تک‌کلیدی سراسری — با احتیاط استفاده کنید.",
	"The capture session ended with a shortcut restore warning.": "نشست ضبط با هشدار بازگردانی میان‌بر پایان یافت.",
	"Rayan Lamp could not prepare shortcut recording.": "چراغ رایان نتوانست ضبط میان‌بر را آماده کند.",
	"The shortcut could not be applied.": "میان‌بر اعمال نشد.",
	"Rayan Lamp could not verify that shortcut. Try again.": "چراغ رایان نتوانست میان‌بر را تأیید کند؛ دوباره تلاش کنید.",
	"Windows or another application already owns this shortcut.": "این میان‌بر در اختیار ویندوز یا برنامهٔ دیگری است.",
	"The shortcut could not be applied. Press it again to retry.": "میان‌بر اعمال نشد؛ برای تلاش دوباره آن را فشار دهید.",
	"The shortcut could not be cleared.": "میان‌بر پاک نشد.",
	"The new stored shortcut needs attention.": "میان‌بر تازهٔ ذخیره‌شده نیازمند توجه است.",
	"{keys} is now assigned.": "{keys} اکنون اختصاص یافته است.",
	"The shortcut is now disabled.": "میان‌بر اکنون غیرفعال است.",
	"Shortcut changed elsewhere. Recording cancelled. {assignment}": "میان‌بر در جای دیگری تغییر کرد و ضبط لغو شد. {assignment}",
	"“{key}” is not supported for a global shortcut.": "کلید «{key}» برای میان‌بر سراسری پشتیبانی نمی‌شود.",
	"That key": "آن کلید",
	"F12 is reserved by Windows. Choose another shortcut.": "کلید F12 در ویندوز رزرو شده است؛ میان‌بر دیگری انتخاب کنید.",
	"Shortcut capture paused because its lease could not be renewed. Select Record to try again.": "ضبط میان‌بر به‌دلیل تمدید نشدن مجوز موقت متوقف شد. برای تلاش دوباره «ضبط» را انتخاب کنید.",
	"The shortcut was applied, but Windows reported a shortcut restore warning.": "میان‌بر اعمال شد، اما ویندوز دربارهٔ بازگردانی میان‌بر هشدار داد.",
	"Recording ended, but Windows reported a shortcut restore warning.": "ضبط پایان یافت، اما ویندوز دربارهٔ بازگردانی میان‌بر هشدار داد.",
	"This recorder no longer owns the capture session.": "این ضبط‌کننده دیگر مالک نشست ضبط نیست.",
	"The shortcut capture session ended.": "نشست ضبط میان‌بر پایان یافت.",
	"Rayan Lamp could not verify that shortcut. Select Record to try again.": "چراغ رایان نتوانست میان‌بر را تأیید کند. برای تلاش دوباره «ضبط» را انتخاب کنید.",
	"The shortcut capture session expired. Select Record to try again.": "نشست ضبط میان‌بر منقضی شد. برای تلاش دوباره «ضبط» را انتخاب کنید.",
	"Event automation": "خودکارسازی رویدادها",
	"Run reviewed local scripts after lamp events and Windows session, power, or network changes.": "پس از رویدادهای چراغ یا تغییرات نشست، توان و شبکهٔ ویندوز، اسکریپت‌های محلی تأییدشده را اجرا می‌کند.",
	"Scripts run with your Windows account and are not sandboxed. Use trusted absolute paths and never evaluate firmware message text as code.": "اسکریپت‌ها با حساب ویندوز شما و بدون sandbox اجرا می‌شوند. فقط از مسیرهای مطلق مورد اعتماد استفاده کنید و متن پیام میان‌افزار را هرگز به‌عنوان کد ارزیابی نکنید.",
	"Runner": "اجراکننده",
	"Enable automation": "فعال‌سازی خودکارسازی",
	"On script": "اسکریپت روشن شدن",
	"Off script": "اسکریپت خاموش شدن",
	"Message script": "اسکریپت پیام",
	"Windows lock script": "اسکریپت قفل ویندوز",
	"Windows unlock script": "اسکریپت باز شدن قفل ویندوز",
	"System suspend script": "اسکریپت تعلیق سیستم",
	"System resume script": "اسکریپت ادامهٔ سیستم",
	"Network-change script": "اسکریپت تغییر شبکه",
	"Runs after Windows reports an IP-interface change.": "پس از گزارش تغییر رابط IP توسط ویندوز اجرا می‌شود.",
	"Working directory": "پوشهٔ کاری",
	"Execution timeout": "مهلت اجرا",
	"From 1s through 10m.": "از ۱ ثانیه تا ۱۰ دقیقه.",
	"Capture script output": "ثبت خروجی اسکریپت",
	"Bounded stdout/stderr is written to the application log.": "خروجی محدود stdout/stderr در گزارش برنامه نوشته می‌شود.",
	"Windows and local integrations": "یکپارچگی‌های ویندوز و محلی",
	"The authenticated API is restricted to the current machine.": "API احراز‌شده به همین رایانه محدود است.",
	"Windows": "ویندوز",
	"Start with Windows": "شروع همراه ویندوز",
	"Current executable registration": "ثبت فایل اجرایی فعلی",
	"Portable mode stays unregistered by default. These one-time actions affect only the executable shown below; they are not saved as settings or replayed at startup.": "حالت قابل‌حمل به‌طور پیش‌فرض ثبت‌نشده می‌ماند. این اقدامات یک‌باره فقط بر فایل اجرایی زیر اثر دارند؛ به‌عنوان تنظیم ذخیره یا هنگام شروع تکرار نمی‌شوند.",
	"Current executable": "فایل اجرایی فعلی",
	"Reading registration…": "در حال خواندن وضعیت ثبت…",
	"Not available": "در دسترس نیست",
	"Registration status unavailable": "وضعیت ثبت در دسترس نیست",
	"The current executable's registration status could not be read.": "وضعیت ثبت فایل اجرایی فعلی خوانده نشد.",
	"Partially completed": "بخشی انجام شد",
	"Registration was not changed": "وضعیت ثبت تغییر نکرد",
	"Windows shell": "پوستهٔ ویندوز",
	"Windows shell integration": "یکپارچگی پوستهٔ ویندوز",
	"links and useful Jump List commands": "پیوندها و فرمان‌های کاربردی Jump List",
	"Register shell": "ثبت پوسته",
	"Unregister shell": "لغو ثبت پوسته",
	"COM automation": "خودکارسازی COM",
	"Opt-in control for VBA, AutoHotkey, and other COM clients": "کنترل اختیاری برای VBA، AutoHotkey و دیگر کارخواه‌های COM",
	"Register COM": "ثبت COM",
	"Unregister COM": "لغو ثبت COM",
	"If this portable executable is moved, register its new path again. Unregistration only removes entries still owned by this exact executable.": "اگر این فایل اجرایی قابل‌حمل جابه‌جا شد، مسیر تازه را دوباره ثبت کنید. لغو ثبت تنها مدخل‌هایی را حذف می‌کند که همچنان متعلق به همین فایل اجرایی‌اند.",
	"Loopback API": "API لوپ‌بک",
	"Enable local API and web dashboard": "فعال‌سازی API محلی و داشبورد وب",
	"Listen address": "نشانی شنونده",
	"Changing this address restarts the listener and may move this dashboard.": "تغییر این نشانی، شنونده را راه‌اندازی دوباره می‌کند و ممکن است نشانی داشبورد را تغییر دهد.",
	"Dashboard address changed": "نشانی داشبورد تغییر کرده است",
	"Validate, apply, and continue at the new loopback origin.": "اعتبارسنجی و اعمال کنید و در مبدأ loopback تازه ادامه دهید.",
	"Apply and move dashboard": "اعمال و انتقال داشبورد",
	"API secret protected": "راز API محافظت می‌شود",
	"The web session never receives or stores the long-lived bearer token.": "نشست وب هرگز توکن بلندمدت bearer را دریافت یا ذخیره نمی‌کند.",
	"Unregister {name}?": "ثبت {name} لغو شود؟",
	"This removes the rayan-lamp URI handler and Jump List tasks owned by this executable.": "این کار کنترل‌کنندهٔ URI ‏rayan-lamp و وظایف Jump List متعلق به این فایل اجرایی را حذف می‌کند.",
	"This removes the current-user COM registration owned by this executable.": "این کار ثبت COM کاربر فعلی متعلق به این فایل اجرایی را حذف می‌کند.",
	"Rayan Lamp settings and device data are not removed.": "تنظیمات چراغ رایان و داده‌های دستگاه حذف نمی‌شوند.",
	"Current executable:": "فایل اجرایی فعلی:",
	"Keep registered": "ثبت حفظ شود",
	"Unregister": "لغو ثبت",
	"Move the local dashboard?": "داشبورد محلی منتقل شود؟",
	"Rayan Lamp will restart its loopback listener at": "چراغ رایان شنوندهٔ loopback را در این نشانی راه‌اندازی دوباره می‌کند:",
	"This tab will continue at the new address.": "این زبانه در نشانی تازه ادامه می‌دهد.",
	"The listener address was not accepted.": "نشانی شنونده پذیرفته نشد.",
	"Listener was not moved": "شنونده منتقل نشد",
	"Listener is moving": "شنونده در حال انتقال است",
	"Apply and continue": "اعمال و ادامه",
	"Registered": "ثبت‌شده",
	"Not registered": "ثبت‌نشده",
	"Windows shell integration registered": "یکپارچگی پوستهٔ ویندوز ثبت شد",
	"Windows shell integration unregistered": "ثبت یکپارچگی پوستهٔ ویندوز لغو شد",
	"COM automation registered": "خودکارسازی COM ثبت شد",
	"COM automation unregistered": "ثبت خودکارسازی COM لغو شد",
	"Windows rejected the registration change.": "ویندوز تغییر ثبت را نپذیرفت.",
	"Shell registration partially completed": "ثبت پوسته بخشی انجام شد",
	"Registration change failed": "تغییر ثبت ناموفق بود",
	"Diagnostic endpoint policy": "سیاست پایانه‌های عیب‌یابی",
	"Only GET endpoints listed here can be queried through the app.": "تنها پایانه‌های GET فهرست‌شده در اینجا از راه برنامه قابل درخواست‌اند.",
	"Audited endpoints": "پایانه‌های بررسی‌شده",
	"The firmware audit fixes this allowlist. Unrelated project and internal debug routes are intentionally unavailable.": "بازبینی میان‌افزار این فهرست مجاز را ثابت می‌کند. مسیرهای پروژه‌های نامرتبط و اشکال‌زدایی داخلی عمداً در دسترس نیستند.",
	"This configuration contains legacy endpoint entries. The web dashboard never presents or requests them.": "این پیکربندی دارای مدخل‌های قدیمی پایانه است. داشبورد وب هرگز آن‌ها را نمایش یا درخواست نمی‌کند.",
	"Enter a loopback host and numeric port.": "میزبان loopback و درگاه عددی را وارد کنید.",
	"Use an address such as 127.0.0.1:47831.": "از نشانی‌ای مانند ‎127.0.0.1:47831 استفاده کنید.",
	"Use only a loopback host and explicit numeric port.": "فقط میزبان loopback و درگاه عددی صریح وارد کنید.",
	"Port must be between 1 and 65535.": "درگاه باید بین ۱ و ۶۵۵۳۵ باشد.",
	"The listener must remain on localhost or a loopback IP.": "شنونده باید روی localhost یا IP لوپ‌بک باقی بماند.",
	"Diagnostics unavailable": "عیب‌یابی در دسترس نیست",
	"Diagnostics refreshed with a warning": "عیب‌یابی با هشدار تازه شد",
	"Diagnostics refreshed": "عیب‌یابی تازه شد",
	"Refresh failed": "تازه‌سازی ناموفق بود",
	"Read-only device data": "داده‌های فقط‌خواندنی دستگاه",
	"Parsed, redacted information from the firmware-audited endpoint set.": "اطلاعات تجزیه‌شده و پاک‌سازی‌شده از مجموعه پایانه‌های بازبینی‌شدهٔ میان‌افزار.",
	"Refresh all": "تازه‌سازی همه",
	"Reading cached device information…": "در حال خواندن اطلاعات ذخیره‌شدهٔ دستگاه…",
	"Network": "شبکه",
	"Runtime": "زمان اجرا",
	"Firmware update": "به‌روزرسانی میان‌افزار",
	"Endpoint health": "سلامت پایانه‌ها",
	"Audited sources": "منابع بازبینی‌شده",
	"Updated {date}": "به‌روزرسانی {date}",
	"Advanced raw response": "پاسخ خام پیشرفته",
	"Cached redacted output by default": "خروجی پاک‌سازی‌شدهٔ ذخیره‌شده به‌طور پیش‌فرض",
	"The cached response below is the backend's redacted SafeRaw projection. An explicit exact fetch is available with a sensitivity warning and is always rendered as plain text.": "پاسخ ذخیره‌شدهٔ زیر نمای SafeRaw پاک‌سازی‌شدهٔ بک‌اند است. دریافت صریح پاسخ دقیق با هشدار حساسیت در دسترس است و همیشه به‌صورت متن ساده نمایش داده می‌شود.",
	"Endpoint": "پایانه",
	"Fetch exact raw": "دریافت خام دقیق",
	"Exact device response: review before copying or sharing. It may contain sensitive identifiers or network data.": "پاسخ دقیق دستگاه: پیش از کپی یا اشتراک بررسی کنید؛ ممکن است شامل شناسه‌های حساس یا داده‌های شبکه باشد.",
	"No cached redacted response is available for this endpoint.": "برای این پایانه پاسخ پاک‌سازی‌شده‌ای در حافظه نیست.",
	"Fetch the exact device response?": "پاسخ دقیق دستگاه دریافت شود؟",
	"This bypasses the cached redacted view. The response may contain sensitive device identifiers or local network information.": "این کار نمای پاک‌سازی‌شدهٔ ذخیره‌شده را دور می‌زند. پاسخ ممکن است شناسه‌های حساس دستگاه یا اطلاعات شبکهٔ محلی را در بر داشته باشد.",
	"Keep redacted view": "حفظ نمای پاک‌سازی‌شده",
	"Unable to fetch endpoint.": "پایانه دریافت نشد.",
	"Fetch exact response": "دریافت پاسخ دقیق",
	"No diagnostic snapshot": "تصویر عیب‌یابی موجود نیست",
	"The controller has not returned parsed device information yet.": "کنترل‌گر هنوز اطلاعات تجزیه‌شدهٔ دستگاه را برنگردانده است.",
	"Not reported": "گزارش نشده",
	"Cached result": "نتیجهٔ ذخیره‌شده",
	"Fetched {date}": "دریافت‌شده در {date}",
	"Not fetched": "دریافت نشده",
	"Protected": "محافظت‌شده",
	"Name": "نام",
	"Hostname": "نام میزبان",
	"Model": "مدل",
	"State": "وضعیت",
	"Wi-Fi": "وای‌فای",
	"SSID": "SSID",
	"Address": "نشانی",
	"Station status": "وضعیت Station",
	"Access point": "نقطهٔ دسترسی",
	"AP address": "نشانی AP",
	"Signal": "سیگنال",
	"Country": "کشور",
	"Uptime": "زمان فعالیت",
	"Free heap": "حافظهٔ آزاد",
	"Program usage": "مصرف برنامه",
	"Protocol": "پروتکل",
	"Firmware integrity": "یکپارچگی میان‌افزار",
	"Attention required": "نیازمند توجه",
	"Verified": "تأییدشده",
	"Build": "ساخت",
	"Hash": "هش",
	"Image size": "اندازهٔ تصویر",
	"Free space": "فضای آزاد",
	"Date": "تاریخ",
	"Yes": "بله",
	"No": "خیر",
	"On": "روشن",
	"Off": "خاموش",
	"on": "روشن",
	"off": "خاموش",
	"unknown": "نامشخص",
	"Enabled": "فعال",
	"Disabled": "غیرفعال",
	"Excellent": "عالی",
	"Good": "خوب",
	"Fair": "متوسط",
	"Poor": "ضعیف",
	"{count}d": "{count} روز",
	"{count}h": "{count} ساعت",
	"{count}m": "{count} دقیقه"
};
function normalizeLocalePreference(value) {
	return value === "en" || value === "fa" || value === "auto" ? value : "auto";
}
function detectLocale(languages = browserLanguages()) {
	return languages.some((language) => language.toLowerCase().startsWith("fa")) ? "fa" : "en";
}
function applyWebLocale(value) {
	const preference = normalizeLocalePreference(value);
	const resolved = preference === "auto" ? detectLocale() : preference;
	localePreference.value = preference;
	resolvedLocale.value = resolved;
	const root = document.documentElement;
	root.lang = resolved === "fa" ? "fa-IR" : "en";
	root.dir = resolved === "fa" ? "rtl" : "ltr";
	root.dataset.locale = preference;
	root.dataset.resolvedLocale = resolved;
	document.title = t("Rayan Lamp");
	document.querySelector("meta[name=\"description\"]")?.setAttribute("content", t("Private local control surface for Rayan Lamp"));
	document.querySelector("meta[name=\"application-name\"]")?.setAttribute("content", t("Rayan Lamp"));
	document.querySelector("meta[name=\"apple-mobile-web-app-title\"]")?.setAttribute("content", t("Rayan Lamp"));
	try {
		window.localStorage.setItem(STORAGE_KEY, preference);
	} catch {}
}
function startWebLocaleController() {
	const onLanguageChange = () => {
		if (localePreference.value === "auto") applyWebLocale("auto");
	};
	window.addEventListener("languagechange", onLanguageChange);
	return () => window.removeEventListener("languagechange", onLanguageChange);
}
function t(source, values) {
	const translated = resolvedLocale.value === "fa" ? fa[source] ?? source : source;
	if (!values) return translated;
	return translated.replace(/\{(\w+)\}/g, (match, key) => {
		const value = values[key];
		return value === void 0 ? match : String(value);
	});
}
function formatNumber(value, options) {
	return new Intl.NumberFormat(localeTag(), {
		numberingSystem: resolvedLocale.value === "fa" ? "arabext" : "latn",
		...options
	}).format(value);
}
function formatDateTime(value, options) {
	return new Intl.DateTimeFormat(localeTag(), {
		calendar: resolvedLocale.value === "fa" ? "persian" : "gregory",
		numberingSystem: resolvedLocale.value === "fa" ? "arabext" : "latn",
		...options
	}).format(value);
}
function BidiLtr({ children, class: className = "" }) {
	return /* @__PURE__ */ u("bdi", {
		dir: "ltr",
		class: `bidi-ltr ${className}`.trim(),
		children
	});
}
function localeTag() {
	return resolvedLocale.value === "fa" ? "fa-IR" : "en-US";
}
function browserLanguages() {
	if (typeof navigator === "undefined") return ["en"];
	return navigator.languages?.length ? navigator.languages : [navigator.language || "en"];
}
//#endregion
//#region src/store.ts
var appPhase = y$1("booting");
var snapshot = y$1(emptySnapshot);
var config = y$1(null);
var version = y$1(null);
var capabilities = y$1({});
var diagnostics = y$1(null);
var revision = y$1(0);
var route = y$1("dashboard");
var settingsSection = y$1("lamp");
var settingsSaving = y$1(false);
var settingsError = y$1("");
var eventConnected = y$1(false);
var controllerAvailable = y$1(false);
var activeClientKind = y$1("auto");
var activeClientLabel = y$1("Selecting connection");
var commandPending = y$1("");
var toasts = y$1([]);
var toastSerial = 0;
var isOnline = g$1(() => controllerAvailable.value && (snapshot.value.http_reachable || snapshot.value.websocket_online));
var stateLabel = g$1(() => {
	if (!isOnline.value) return t("Lamp unavailable");
	switch (snapshot.value.state) {
		case "on": return t("Lamp is on");
		case "off": return t("Lamp is off");
		default: return t("Checking lamp");
	}
});
function setWebTheme(theme) {
	applyThemeLocally(theme);
	if (config.value?.ui.theme !== theme) updateConfig({ ui: { theme } }, true);
}
function setWebLocale(locale) {
	applyLocaleLocally(locale);
	if ((config.value?.ui.locale ?? "auto") !== locale) updateConfig({ ui: { locale } }, true);
}
function applyThemeLocally(theme) {
	applyWebTheme(theme);
}
function applyLocaleLocally(locale) {
	applyWebLocale(locale);
}
function applyMotionLocally(enabled) {
	document.documentElement.dataset.motion = enabled ? "on" : "off";
}
function showToast(tone, title, message, duration = 4500) {
	const item = {
		id: ++toastSerial,
		tone,
		title,
		message
	};
	toasts.value = [...toasts.value.slice(-3), item];
	window.setTimeout(() => dismissToast(item.id), duration);
}
function dismissToast(id) {
	toasts.value = toasts.value.filter((item) => item.id !== id);
}
function applyBootstrap(value) {
	if (!value || typeof value !== "object") throw new Error("The controller returned an invalid bootstrap document.");
	const candidate = value;
	const nextSnapshot = candidate.snapshot ?? candidate.status;
	if (!isSnapshot(nextSnapshot)) throw new Error("The controller bootstrap did not contain a valid lamp snapshot.");
	const nextConfig = candidate.config;
	if (!isAppConfig(nextConfig)) throw new Error("The controller bootstrap did not contain a valid configuration.");
	const nextVersion = candidate.version && typeof candidate.version === "object" ? candidate.version : null;
	n(() => {
		snapshot.value = nextSnapshot;
		config.value = nextConfig;
		applyThemeLocally(nextConfig.ui.theme);
		applyLocaleLocally(nextConfig.ui.locale ?? "auto");
		applyMotionLocally(nextConfig.ui.animations);
		if (nextVersion) version.value = nextVersion;
		revision.value = typeof candidate.revision === "string" || typeof candidate.revision === "number" ? candidate.revision : revision.value;
		capabilities.value = candidate.capabilities && typeof candidate.capabilities === "object" ? candidate.capabilities : {};
		diagnostics.value = candidate.diagnostics && typeof candidate.diagnostics === "object" ? candidate.diagnostics : diagnostics.value;
		controllerAvailable.value = true;
		appPhase.value = derivePhase(nextSnapshot);
	});
	browserConsole.event(nextSnapshot.state === "unknown" ? "info" : "success", "lamp.state", "Initial lamp state synchronized", {
		source: nextSnapshot.last_source || "bootstrap",
		state: nextSnapshot.state
	}, {
		dedupeKey: `bootstrap:${nextSnapshot.state}:${nextSnapshot.last_source}`,
		dedupeWindowMs: 2e3
	});
}
function beginControllerConnection() {
	n(() => {
		eventConnected.value = false;
		controllerAvailable.value = false;
		appPhase.value = "booting";
	});
}
function applySnapshot(next) {
	const previousState = snapshot.value.state;
	n(() => {
		snapshot.value = next;
		controllerAvailable.value = true;
		appPhase.value = derivePhase(next);
	});
	if (next.state !== previousState) browserConsole.event(next.state === "unknown" ? "warn" : "success", "lamp.state", `Lamp changed from ${previousState} to ${next.state}`, {
		source: next.last_source || "state-update",
		state: next.state
	});
}
function markControllerAvailable() {
	n(() => {
		controllerAvailable.value = true;
		appPhase.value = derivePhase(snapshot.value);
	});
}
function markControllerUnavailable(reason = "The RayanLamp controller is unavailable.") {
	n(() => {
		controllerAvailable.value = false;
		eventConnected.value = false;
		snapshot.value = {
			...snapshot.value,
			http_reachable: false,
			websocket_online: false,
			reconnect_attempt: Math.max(1, snapshot.value.reconnect_attempt),
			last_error: reason
		};
		appPhase.value = "offline";
	});
}
function derivePhase(value) {
	if (value.websocket_online && value.http_reachable) return "online";
	if (value.websocket_online || value.http_reachable) return "degraded";
	if (value.reconnect_attempt > 0) return "connecting";
	return "offline";
}
function mergeRecord(base, patch) {
	if (!base || !patch || typeof base !== "object" || typeof patch !== "object" || Array.isArray(base) || Array.isArray(patch)) return patch;
	const output = { ...base };
	for (const [key, value] of Object.entries(patch)) {
		if (value === void 0) continue;
		const current = output[key];
		output[key] = current && value && typeof current === "object" && typeof value === "object" && !Array.isArray(current) && !Array.isArray(value) ? mergeRecord(current, value) : value;
	}
	return output;
}
var apiClient = null;
var pendingPatch = {};
var patchTimer = 0;
var patchInFlight = null;
var persistenceGeneration = 0;
function configureSettingsPersistence(api) {
	if (apiClient === api) return;
	persistenceGeneration++;
	window.clearTimeout(patchTimer);
	pendingPatch = {};
	settingsSaving.value = false;
	settingsError.value = "";
	apiClient = api;
}
function updateConfig(patch, immediate = false) {
	if (!config.value) return;
	config.value = mergeRecord(config.value, patch);
	const nextTheme = patch.ui?.theme;
	if (nextTheme === "system" || nextTheme === "light" || nextTheme === "dark") applyThemeLocally(nextTheme);
	const nextLocale = patch.ui?.locale;
	if (nextLocale === "auto" || nextLocale === "en" || nextLocale === "fa") applyLocaleLocally(nextLocale);
	if (typeof patch.ui?.animations === "boolean") applyMotionLocally(patch.ui.animations);
	pendingPatch = mergeRecord(pendingPatch, patch);
	settingsError.value = "";
	window.clearTimeout(patchTimer);
	patchTimer = window.setTimeout(flushConfigPatch, immediate ? 0 : 500);
}
async function flushConfigPatch() {
	window.clearTimeout(patchTimer);
	while (apiClient) {
		if (patchInFlight) {
			await patchInFlight;
			if (settingsError.value) return;
			continue;
		}
		if (Object.keys(pendingPatch).length === 0) return;
		const patch = pendingPatch;
		pendingPatch = {};
		const client = apiClient;
		const generation = persistenceGeneration;
		const sections = Object.keys(patch).sort().join(",");
		const saveStarted = performance.now();
		const request = (async () => {
			settingsSaving.value = true;
			try {
				const result = await client.patchConfig(patch, revision.value);
				if (generation !== persistenceGeneration || apiClient !== client) return;
				const stillPending = pendingPatch;
				n(() => {
					config.value = mergeRecord(result.config, stillPending);
					applyLocaleLocally(config.value.ui.locale ?? localePreference.value);
					applyMotionLocally(config.value.ui.animations);
					revision.value = result.revision;
					settingsError.value = "";
				});
				browserConsole.event("success", "settings.saved", "Settings synchronized", {
					source: client.kind,
					action: sections,
					elapsed_ms: Math.round(performance.now() - saveStarted)
				});
			} catch (error) {
				if (generation !== persistenceGeneration || apiClient !== client) return;
				pendingPatch = mergeRecord(patch, pendingPatch);
				const message = error instanceof Error ? error.message : t("Unable to save settings.");
				settingsError.value = message;
				browserConsole.event("error", "settings.error", "Settings could not be synchronized", {
					source: client.kind,
					action: sections,
					elapsed_ms: Math.round(performance.now() - saveStarted),
					...browserErrorMetadata(error)
				});
				showToast("error", t("Settings were not saved"), t(message));
				try {
					const result = await client.getConfig();
					config.value = mergeRecord(result.config, pendingPatch);
					applyLocaleLocally(config.value.ui.locale ?? localePreference.value);
					applyMotionLocally(config.value.ui.animations);
					revision.value = result.revision;
				} catch {}
			} finally {
				if (generation === persistenceGeneration && apiClient === client) settingsSaving.value = false;
			}
		})();
		patchInFlight = request;
		try {
			await request;
		} finally {
			if (patchInFlight === request) patchInFlight = null;
		}
		if (settingsError.value) return;
	}
}
function acceptRemoteConfig(next, nextRevision) {
	config.value = mergeRecord(next, pendingPatch);
	revision.value = nextRevision;
	applyThemeLocally(config.value.ui.theme);
	applyLocaleLocally(config.value.ui.locale ?? "auto");
	applyMotionLocally(config.value.ui.animations);
}
//#endregion
//#region src/favicon.ts
var faviconURLs = {
	on: "data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%2064%2064'%3e%3cdefs%3e%3clinearGradient%20id='glass'%20x1='0'%20y1='0'%20x2='.8'%20y2='1'%3e%3cstop%20offset='0'%20stop-color='%23fffbd5'/%3e%3cstop%20offset='1'%20stop-color='%23ffbc46'/%3e%3c/linearGradient%3e%3c/defs%3e%3cg%20fill='none'%20stroke='%23ffd86a'%20stroke-width='3'%20stroke-linecap='round'%3e%3cpath%20d='M32%202v5M14%208l4%204M50%208l-4%204M5%2025l6%201M59%2025l-6%201'/%3e%3c/g%3e%3cpath%20d='M32%209C21%209%2013%2017%2013%2028c0%208%204%2013%208%2017%202%202%203%204%203%207h16c0-3%201-5%203-7%204-4%208-9%208-17%200-11-8-19-19-19Z'%20fill='url(%23glass)'%20stroke='%238de1ff'%20stroke-width='2.5'/%3e%3cpath%20d='m27%2037%205-7%205%207M32%2030v13'%20fill='none'%20stroke='%23fff6b5'%20stroke-width='2.4'%20stroke-linecap='round'%20stroke-linejoin='round'/%3e%3cpath%20d='M24%2050h16v5c0%202-2%203-4%203h-8c-2%200-4-1-4-3Z'%20fill='%2343515e'%20stroke='%23afc0ce'%20stroke-width='1.8'/%3e%3cpath%20d='M27%2059h10c-1%203-3%204-5%204s-4-1-5-4Z'%20fill='%231b2732'/%3e%3c/svg%3e",
	off: "data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%2064%2064'%3e%3cdefs%3e%3clinearGradient%20id='glass'%20x1='0'%20y1='0'%20x2='.8'%20y2='1'%3e%3cstop%20offset='0'%20stop-color='%23315b7f'/%3e%3cstop%20offset='1'%20stop-color='%230d1f33'/%3e%3c/linearGradient%3e%3c/defs%3e%3cpath%20d='M32%209C21%209%2013%2017%2013%2028c0%208%204%2013%208%2017%202%202%203%204%203%207h16c0-3%201-5%203-7%204-4%208-9%208-17%200-11-8-19-19-19Z'%20fill='url(%23glass)'%20stroke='%238de1ff'%20stroke-width='2.5'/%3e%3cpath%20d='m27%2037%205-7%205%207M32%2030v13'%20fill='none'%20stroke='%237ed9ff'%20stroke-width='2.4'%20stroke-linecap='round'%20stroke-linejoin='round'%20opacity='.55'/%3e%3cpath%20d='M24%2050h16v5c0%202-2%203-4%203h-8c-2%200-4-1-4-3Z'%20fill='%2343515e'%20stroke='%23afc0ce'%20stroke-width='1.8'/%3e%3cpath%20d='M27%2059h10c-1%203-3%204-5%204s-4-1-5-4Z'%20fill='%231b2732'/%3e%3c/svg%3e",
	offline: "data:image/svg+xml,%3csvg%20xmlns='http://www.w3.org/2000/svg'%20viewBox='0%200%2064%2064'%3e%3cdefs%3e%3clinearGradient%20id='glass'%20x1='0'%20y1='0'%20x2='.8'%20y2='1'%3e%3cstop%20offset='0'%20stop-color='%23566575'/%3e%3cstop%20offset='1'%20stop-color='%2318232e'/%3e%3c/linearGradient%3e%3c/defs%3e%3cpath%20d='M30%209C19%209%2011%2017%2011%2028c0%208%204%2013%208%2017%202%202%203%204%203%207h16c0-3%201-5%203-7%204-4%208-9%208-17%200-11-8-19-19-19Z'%20fill='url(%23glass)'%20stroke='%2391a5b8'%20stroke-width='2.5'/%3e%3cpath%20d='m25%2037%205-7%205%207M30%2030v13'%20fill='none'%20stroke='%23a8b7c5'%20stroke-width='2.4'%20stroke-linecap='round'%20stroke-linejoin='round'%20opacity='.5'/%3e%3cpath%20d='M22%2050h16v5c0%202-2%203-4%203h-8c-2%200-4-1-4-3Z'%20fill='%233b4753'%20stroke='%238fa0ae'%20stroke-width='1.8'/%3e%3cpath%20d='M25%2059h10c-1%203-3%204-5%204s-4-1-5-4Z'%20fill='%2318212a'/%3e%3ccircle%20cx='50'%20cy='49'%20r='12'%20fill='%23d94452'%20stroke='%23fff'%20stroke-width='2'/%3e%3cpath%20d='m45%2044%2010%2010M55%2044%2045%2054'%20fill='none'%20stroke='%23fff'%20stroke-width='3'%20stroke-linecap='round'/%3e%3c/svg%3e",
	connecting: "" + new URL("favicon-connecting-Dv7S5MsW.svg", import.meta.url).href
};
function faviconURL(state) {
	return faviconURLs[state];
}
function faviconState(state, phase, available) {
	if (phase === "booting" || phase === "connecting") return "connecting";
	if (!available || phase === "offline") return "offline";
	if (state === "unknown") return "connecting";
	return state;
}
function startWebFaviconController(target = document) {
	return j(() => {
		updateFavicon(faviconState(snapshot.value.state, appPhase.value, controllerAvailable.value), target);
	});
}
function updateFavicon(state, target = document) {
	const current = target.querySelector("link[data-rayanlamp-favicon]");
	if (current?.dataset.lampState === state) return;
	const next = target.createElement("link");
	next.rel = "icon";
	next.type = "image/svg+xml";
	next.setAttribute("sizes", "any");
	next.dataset.rayanlampFavicon = "";
	next.dataset.lampState = state;
	next.href = new URL(faviconURL(state), target.baseURI).href;
	if (current) current.replaceWith(next);
	else target.head.append(next);
}
//#endregion
//#region src/haptics.ts
var fixedPatterns = {
	"command.start": 12,
	"command.success": [
		15,
		35,
		24
	],
	"command.error": [
		28,
		42,
		28
	],
	"message.received": [
		18,
		55,
		18
	]
};
var WebHaptics = class {
	runtime;
	constructor(runtime = browserHapticsRuntime) {
		this.runtime = runtime;
	}
	get supported() {
		return typeof this.runtime.vibrate === "function";
	}
	trigger(cue) {
		return this.play(fixedPatterns[cue]);
	}
	audioCue(count) {
		if (!Number.isFinite(count) || count < 1) return false;
		const pulseCount = Math.min(3, Math.floor(count));
		const pattern = [];
		for (let index = 0; index < pulseCount; index += 1) {
			if (index > 0) pattern.push(65);
			pattern.push(20);
		}
		return this.play(pattern);
	}
	play(pattern) {
		const vibrate = this.runtime.vibrate;
		if (typeof vibrate !== "function" || !safeCondition(this.runtime.isDocumentVisible, false) || safeCondition(this.runtime.prefersReducedMotion, true)) return false;
		try {
			return Boolean(vibrate(typeof pattern === "number" ? pattern : Array.from(pattern)));
		} catch {
			return false;
		}
	}
};
function safeCondition(read, fallback) {
	try {
		return read();
	} catch {
		return fallback;
	}
}
var browserHapticsRuntime = {
	vibrate: typeof navigator !== "undefined" && typeof navigator.vibrate === "function" ? (pattern) => navigator.vibrate(pattern) : void 0,
	isDocumentVisible: () => typeof document !== "undefined" && document.visibilityState === "visible",
	prefersReducedMotion: () => typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches
};
var webHaptics = new WebHaptics();
//#endregion
//#region src/sync.ts
var fallbackElectionDelay = 80;
var fallbackLeaseDuration = 12e3;
var fallbackLeaseHeartbeat = 3e3;
var RealtimeSync = class {
	api;
	connectEvents;
	tabID = crypto.randomUUID();
	channel;
	lockName;
	stopped = false;
	socket = null;
	retry = 0;
	heartbeat = 0;
	followerWatchdog = 0;
	reconnectTimer = 0;
	releaseLeader = null;
	ownsLeadership = false;
	leadershipMode = null;
	leaseOwner = "";
	leaseExpiresAt = 0;
	leaseElectionTimer = 0;
	leaseExpiryTimer = 0;
	leaseHeartbeatTimer = 0;
	leaseCandidates = /* @__PURE__ */ new Set();
	lastServerSequence = 0;
	lastLoggedConnection = null;
	directKnownStateSeen = false;
	constructor(api, options = {}) {
		this.api = api;
		this.connectEvents = options.connectEvents !== false;
		const names = connectionSyncNames(api.syncKey);
		this.lockName = names.lock;
		this.channel = typeof BroadcastChannel === "function" ? new BroadcastChannel(names.channel) : null;
	}
	start() {
		if (this.channel) this.channel.addEventListener("message", this.onChannelMessage);
		if (this.connectEvents) {
			this.heartbeat = window.setInterval(() => {
				this.announceConnection();
			}, 5e3);
			this.broadcast({ kind: "heartbeat" });
		}
		this.electLeader();
	}
	stop() {
		this.stopped = true;
		if (this.leadershipMode === "lease" && this.ownsLeadership) this.broadcast({
			kind: "lease",
			leaseAction: "release"
		});
		this.releaseLeader?.();
		this.releaseLeader = null;
		this.releaseLeadership();
		window.clearInterval(this.heartbeat);
		window.clearTimeout(this.followerWatchdog);
		window.clearTimeout(this.reconnectTimer);
		window.clearTimeout(this.leaseElectionTimer);
		window.clearTimeout(this.leaseExpiryTimer);
		window.clearInterval(this.leaseHeartbeatTimer);
		this.channel?.removeEventListener("message", this.onChannelMessage);
		this.channel?.close();
	}
	async electLeader() {
		const locks = navigator.locks;
		if (!locks) {
			if (this.channel) this.beginLeaseElection();
			else this.acquireLeadership("uncoordinated");
			return;
		}
		while (!this.stopped) {
			let acquired = false;
			await locks.request(this.lockName, {
				mode: "exclusive",
				ifAvailable: true
			}, async (lock) => {
				if (!lock || this.stopped) return;
				acquired = true;
				this.acquireLeadership("locks");
				await new Promise((resolve) => {
					this.releaseLeader = resolve;
				});
				this.releaseLeader = null;
				this.releaseLeadership();
			});
			if (!acquired) await new Promise((resolve) => window.setTimeout(resolve, 2e3));
		}
	}
	openSocket() {
		if (this.stopped || !this.ownsLeadership || !this.connectEvents || this.socket) return;
		let socket;
		const attempt = this.retry + 1;
		const connectStarted = performance.now();
		try {
			socket = new WebSocket(this.api.realtimeURL());
		} catch (error) {
			const reason = relayDisconnectReason();
			this.setConnectionState(false, reason, {
				action: "leader",
				attempt,
				elapsed_ms: Math.round(performance.now() - connectStarted),
				...browserErrorMetadata(error)
			});
			this.broadcast({
				kind: "heartbeat",
				connected: false,
				reason
			});
			for (const event of this.api.realtimeClosed()) this.handleServerEvent(event);
			if (!this.stopped) this.scheduleReconnect();
			return;
		}
		this.socket = socket;
		socket.addEventListener("open", () => {
			this.retry = 0;
			window.clearTimeout(this.reconnectTimer);
			this.setConnectionState(true, void 0, {
				action: "leader",
				attempt,
				elapsed_ms: Math.round(performance.now() - connectStarted)
			});
			this.broadcast({
				kind: "heartbeat",
				connected: true
			});
			try {
				for (const event of this.api.realtimeOpened(socket)) this.handleServerEvent(event);
			} catch {
				socket.close();
			}
		});
		socket.addEventListener("message", (event) => {
			this.handleServerMessage(event.data);
		});
		socket.addEventListener("close", (event) => {
			if (this.socket === socket) this.socket = null;
			if (this.stopped || !this.ownsLeadership) return;
			const reason = relayDisconnectReason(event);
			this.setConnectionState(false, reason, {
				action: "leader",
				attempt: this.retry + 1,
				error_code: event.code ? `close_${event.code}` : "socket_closed"
			});
			this.broadcast({
				kind: "heartbeat",
				connected: false,
				reason
			});
			for (const event of this.api.realtimeClosed()) this.handleServerEvent(event);
			if (!this.stopped && this.ownsLeadership) this.scheduleReconnect();
		});
		socket.addEventListener("error", () => {
			socket.close();
		});
	}
	scheduleReconnect() {
		if (this.stopped || !this.ownsLeadership || !this.connectEvents) return;
		const delay = Math.min(1e4, 500 * 2 ** this.retry++) * (.8 + Math.random() * .4);
		window.clearTimeout(this.reconnectTimer);
		this.reconnectTimer = window.setTimeout(() => void this.prepareReconnect(), delay);
	}
	async prepareReconnect() {
		if (this.stopped || !this.ownsLeadership || !this.connectEvents || this.socket) return;
		if (this.api.kind === "relay") try {
			await this.api.establishSession(true);
		} catch (error) {
			if (!this.stopped) {
				this.scheduleReconnect();
				browserConsole.event("warn", "websocket.disconnected", "Coordinator session refresh failed; retry scheduled", {
					source: this.api.kind,
					attempt: this.retry,
					...browserErrorMetadata(error)
				});
			}
			return;
		}
		this.openSocket();
	}
	acquireLeadership(mode) {
		if (this.stopped) return;
		this.ownsLeadership = true;
		this.leadershipMode = mode;
		if (this.connectEvents) this.openSocket();
	}
	releaseLeadership() {
		this.ownsLeadership = false;
		this.leadershipMode = null;
		window.clearTimeout(this.reconnectTimer);
		this.socket?.close(this.stopped ? 1e3 : 1001, this.stopped ? "page closed" : "leadership released");
		this.socket = null;
	}
	beginLeaseElection(announce = true) {
		if (this.stopped || this.ownsLeadership || this.hasActiveLeaseOwner() || this.leaseElectionTimer) return;
		this.leaseCandidates.clear();
		this.leaseCandidates.add(this.tabID);
		if (announce) this.broadcast({
			kind: "lease",
			leaseAction: "candidate"
		});
		this.leaseElectionTimer = window.setTimeout(() => {
			this.leaseElectionTimer = 0;
			this.completeLeaseElection();
		}, fallbackElectionDelay);
	}
	completeLeaseElection() {
		if (this.stopped || this.ownsLeadership || this.hasActiveLeaseOwner()) {
			this.leaseCandidates.clear();
			return;
		}
		const winner = [...this.leaseCandidates].sort()[0] ?? this.tabID;
		this.leaseCandidates.clear();
		if (winner === this.tabID) {
			this.leaseOwner = this.tabID;
			this.leaseExpiresAt = Date.now() + fallbackLeaseDuration;
			this.acquireLeadership("lease");
			this.announceLeaseOwnership();
			window.clearInterval(this.leaseHeartbeatTimer);
			this.leaseHeartbeatTimer = window.setInterval(() => this.announceLeaseOwnership(), fallbackLeaseHeartbeat);
			return;
		}
		this.leaseOwner = winner;
		this.leaseExpiresAt = Date.now() + fallbackElectionDelay * 4;
		this.armLeaseExpiry();
	}
	announceLeaseOwnership() {
		if (this.stopped || !this.ownsLeadership || this.leadershipMode !== "lease") return;
		this.leaseOwner = this.tabID;
		this.leaseExpiresAt = Date.now() + fallbackLeaseDuration;
		this.broadcast({
			kind: "lease",
			leaseAction: "owner"
		});
	}
	handleLeaseMessage(value) {
		if (navigator.locks || !this.channel || this.stopped) return;
		switch (value.leaseAction) {
			case "candidate": {
				if (this.ownsLeadership && this.leadershipMode === "lease") {
					this.announceLeaseOwnership();
					return;
				}
				if (this.hasActiveLeaseOwner()) return;
				const wasNew = !this.leaseCandidates.has(value.sender);
				this.leaseCandidates.add(value.sender);
				if (!this.leaseElectionTimer) {
					this.beginLeaseElection(false);
					this.leaseCandidates.add(value.sender);
				}
				if (wasNew) this.broadcast({
					kind: "lease",
					leaseAction: "candidate"
				});
				return;
			}
			case "owner":
				this.acceptLeaseOwner(value.sender);
				return;
			case "release":
				if (!this.ownsLeadership && this.leaseOwner === value.sender) {
					this.clearLeaseOwner();
					this.beginLeaseElection();
				}
				return;
			default: return;
		}
	}
	acceptLeaseOwner(owner) {
		if (!owner || owner === this.tabID) return;
		if (this.ownsLeadership && this.leadershipMode === "lease") if (owner < this.tabID) {
			window.clearInterval(this.leaseHeartbeatTimer);
			this.releaseLeadership();
		} else {
			this.announceLeaseOwnership();
			return;
		}
		window.clearTimeout(this.leaseElectionTimer);
		this.leaseElectionTimer = 0;
		this.leaseCandidates.clear();
		this.leaseOwner = owner;
		this.leaseExpiresAt = Date.now() + fallbackLeaseDuration;
		this.armLeaseExpiry();
	}
	hasActiveLeaseOwner() {
		return this.leaseOwner !== "" && this.leaseOwner !== this.tabID && this.leaseExpiresAt > Date.now();
	}
	armLeaseExpiry() {
		window.clearTimeout(this.leaseExpiryTimer);
		if (!this.hasActiveLeaseOwner()) {
			this.clearLeaseOwner();
			this.beginLeaseElection();
			return;
		}
		this.leaseExpiryTimer = window.setTimeout(() => {
			this.leaseExpiryTimer = 0;
			if (this.hasActiveLeaseOwner()) {
				this.armLeaseExpiry();
				return;
			}
			this.clearLeaseOwner();
			this.beginLeaseElection();
		}, Math.max(1, this.leaseExpiresAt - Date.now()));
	}
	clearLeaseOwner() {
		this.leaseOwner = "";
		this.leaseExpiresAt = 0;
		window.clearTimeout(this.leaseExpiryTimer);
		this.leaseExpiryTimer = 0;
	}
	setConnectionState(connected, reason, metadata = {}) {
		eventConnected.value = connected;
		if (this.lastLoggedConnection !== connected) {
			this.lastLoggedConnection = connected;
			browserConsole.event(connected ? "success" : "warn", connected ? "websocket.connected" : "websocket.disconnected", connected ? "Realtime events connected" : "Realtime events disconnected; reconnecting", {
				source: this.api.kind,
				...metadata
			});
		}
		if (this.api.kind !== "relay") return;
		if (connected) {
			markControllerAvailable();
			return;
		}
		markControllerUnavailable(reason);
	}
	handleServerMessage(raw) {
		for (const event of this.api.decodeRealtimeMessage(raw)) this.handleServerEvent(event);
	}
	/**
	* Accepts a confirmed snapshot from HTTP polling or a command refresh.
	* Direct-mode transition policy remains owned by the elected tab: followers
	* update immediately, then forward the observation so the owner plays and
	* broadcasts the cue exactly once.
	*/
	acceptSnapshot(next) {
		if (this.api.kind !== "direct") {
			applySnapshot(next);
			return;
		}
		if (this.ownsLeadership) {
			this.applyOwnedDirectSnapshot(next);
			return;
		}
		if (next.state !== "unknown") this.directKnownStateSeen = true;
		applySnapshot(next);
		this.broadcast({
			kind: "snapshot-observed",
			snapshot: next,
			revision: revision.value,
			connected: eventConnected.value
		});
	}
	applyOwnedDirectSnapshot(next) {
		this.handleDirectStateCue(next);
		applySnapshot(next);
		this.broadcast({
			kind: "snapshot",
			snapshot: next,
			revision: revision.value,
			connected: eventConnected.value
		});
	}
	handleServerEvent(event) {
		const sequence = serverSequenceDecision(event.type, event.sequence, this.lastServerSequence);
		if (!sequence.accept) return;
		this.lastServerSequence = sequence.next;
		const eventRevision = event.revision ?? revision.value;
		let recognized = false;
		const projectedSnapshot = event.snapshot && isSnapshot(event.snapshot) ? event.snapshot : isSnapshot(event) ? event : null;
		if (projectedSnapshot) {
			recognized = true;
			this.handleDirectStateCue(projectedSnapshot);
			applySnapshot(projectedSnapshot);
			this.broadcast({
				kind: "snapshot",
				snapshot: projectedSnapshot,
				revision: eventRevision,
				connected: eventConnected.value
			});
		}
		if (event.config && isAppConfig(event.config)) {
			recognized = true;
			acceptRemoteConfig(event.config, eventRevision);
			this.broadcast({
				kind: "config",
				config: event.config,
				revision: eventRevision,
				connected: eventConnected.value
			});
		}
		if (event.diagnostics && typeof event.diagnostics === "object") {
			recognized = true;
			diagnostics.value = event.diagnostics;
			browserConsole.event("success", "diagnostics.loaded", "Live diagnostics synchronized", {
				source: this.api.kind,
				count: Object.keys(event.diagnostics.endpoints ?? {}).length
			});
			this.broadcast({
				kind: "diagnostics",
				diagnostics: event.diagnostics,
				connected: eventConnected.value
			});
		}
		if (event.message) {
			recognized = true;
			const cueCount = audioCueCount(event.result);
			if (cueCount !== null) {
				if (this.api.kind === "direct") this.playDirectCue("message", cueCount);
				webHaptics.audioCue(cueCount);
				browserConsole.event("info", "audio.cue", cueCount === 1 ? "Lamp requested an audible cue" : `Lamp requested ${cueCount} audible cues`, {
					source: this.api.kind,
					action: "beep",
					count: cueCount
				});
			} else {
				if (this.api.kind === "direct" && config.value?.notifications.sound) this.playDirectCue("message");
				webHaptics.trigger("message.received");
				browserConsole.event("info", "message.received", "Lamp message received", {
					source: this.api.kind,
					action: event.title || "Rayan Lamp",
					count: event.message.length
				});
			}
			showToast("info", event.title || "Rayan Lamp", event.message);
			this.broadcast({
				kind: "message",
				title: event.title,
				message: event.message,
				...cueCount !== null ? { cueCount } : {},
				revision: eventRevision,
				connected: eventConnected.value
			});
		}
		if (!recognized && event.type) browserConsole.event("debug", "device.event", "Unprojected device event received", {
			source: this.api.kind,
			action: event.type
		});
	}
	onChannelMessage = (incoming) => {
		const value = incoming.data;
		if (!value || value.protocol !== 1 || value.sender === this.tabID) return;
		if (value.kind === "lease") {
			this.handleLeaseMessage(value);
			return;
		}
		if (value.kind === "snapshot-observed" && value.snapshot && isSnapshot(value.snapshot)) {
			if (this.api.kind === "direct" && this.ownsLeadership) this.applyOwnedDirectSnapshot(value.snapshot);
			return;
		}
		if (value.kind === "heartbeat") {
			if (typeof value.connected === "boolean") {
				this.setConnectionState(value.connected, value.reason, { action: "follower" });
				this.armFollowerWatchdog(value.connected);
			} else if (this.socket?.readyState === WebSocket.OPEN) {
				this.broadcast({
					kind: "heartbeat",
					connected: true
				});
				if (this.api.kind === "direct") this.broadcast({
					kind: "snapshot",
					snapshot: snapshot.value,
					revision: revision.value,
					connected: true
				});
			}
			return;
		}
		const connected = typeof value.connected === "boolean" ? value.connected : true;
		this.setConnectionState(connected, value.reason, { action: "follower" });
		this.armFollowerWatchdog(connected);
		if (value.kind === "snapshot" && value.snapshot && isSnapshot(value.snapshot)) {
			if (this.api.kind === "direct" && value.snapshot.state !== "unknown") this.directKnownStateSeen = true;
			applySnapshot(value.snapshot);
		} else if (value.kind === "config" && value.config && isAppConfig(value.config)) acceptRemoteConfig(value.config, value.revision ?? revision.value);
		else if (value.kind === "diagnostics" && value.diagnostics) {
			diagnostics.value = value.diagnostics;
			browserConsole.event("success", "diagnostics.loaded", "Diagnostics synchronized from another tab", {
				source: this.api.kind,
				action: "follower",
				count: Object.keys(value.diagnostics.endpoints).length
			});
		} else if (value.kind === "message" && value.message) {
			const cueCount = audioCueCount({
				kind: value.cueCount === void 0 ? "message" : "beep",
				count: value.cueCount
			});
			if (cueCount !== null) {
				webHaptics.audioCue(cueCount);
				browserConsole.event("info", "audio.cue", cueCount === 1 ? "Lamp requested an audible cue" : `Lamp requested ${cueCount} audible cues`, {
					source: this.api.kind,
					action: "beep",
					count: cueCount
				});
			} else {
				webHaptics.trigger("message.received");
				browserConsole.event("info", "message.received", "Lamp message synchronized from another tab", {
					source: this.api.kind,
					action: value.title || "Rayan Lamp",
					count: value.message.length
				});
			}
			showToast("info", value.title || "Rayan Lamp", value.message);
		}
	};
	handleDirectStateCue(next) {
		if (this.api.kind !== "direct" || next.state === "unknown") return;
		const settings = config.value?.notifications;
		const initial = !this.directKnownStateSeen;
		const changed = snapshot.value.state !== next.state;
		this.directKnownStateSeen = true;
		if (!settings?.sound || (initial ? !settings.initial_sync : !changed)) return;
		this.playDirectCue(next.state === "on" ? "on" : "off");
	}
	playDirectCue(cue, count = 1) {
		this.api.playAudio(cue, count).catch((error) => {
			browserConsole.event("warn", "audio.error", "Browser audio playback failed", {
				source: this.api.kind,
				action: cue,
				count,
				...browserErrorMetadata(error)
			});
		});
	}
	broadcast(message) {
		this.channel?.postMessage({
			protocol: 1,
			sender: this.tabID,
			...message
		});
	}
	announceConnection() {
		if (this.socket?.readyState === WebSocket.OPEN) {
			this.broadcast({
				kind: "heartbeat",
				connected: true
			});
			return;
		}
		this.broadcast({ kind: "heartbeat" });
	}
	armFollowerWatchdog(connected) {
		window.clearTimeout(this.followerWatchdog);
		if (!connected || this.socket?.readyState === WebSocket.OPEN) return;
		this.followerWatchdog = window.setTimeout(() => {
			if (this.socket?.readyState !== WebSocket.OPEN) this.setConnectionState(false, "The RayanLamp controller heartbeat stopped.", {
				action: "follower",
				error_code: "heartbeat_timeout"
			});
		}, 12e3);
	}
};
function audioCueCount(result) {
	if (!result || typeof result !== "object") return null;
	const candidate = result;
	if (candidate.kind !== "beep" || typeof candidate.count !== "number" || !Number.isInteger(candidate.count) || candidate.count < 1) return null;
	return Math.min(candidate.count, 10);
}
function relayDisconnectReason(event) {
	if (event?.code === 1001 && event.reason.trim().toLowerCase() === "server shutting down") return "The RayanLamp controller shut down.";
	if (event?.wasClean) return "The RayanLamp controller closed its live connection.";
	return "The RayanLamp controller connection was lost.";
}
function connectionSyncNames(key) {
	let hash = 2166136261;
	for (let index = 0; index < key.length; index++) {
		hash ^= key.charCodeAt(index);
		hash = Math.imul(hash, 16777619);
	}
	const suffix = (hash >>> 0).toString(16).padStart(8, "0");
	return {
		channel: `rayanlamp-web-v1-${suffix}`,
		lock: `rayanlamp-web-event-leader-v1-${suffix}`
	};
}
function serverSequenceDecision(type, incoming, previous) {
	if (typeof incoming !== "number") return {
		accept: true,
		next: previous
	};
	if (type === "hello") return {
		accept: true,
		next: incoming
	};
	if (incoming <= previous) return {
		accept: false,
		next: previous
	};
	return {
		accept: true,
		next: incoming
	};
}
//#endregion
//#region src/components/icon.tsx
var paths = {
	lamp: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M12 2.75a6.75 6.75 0 0 0-4.2 12.03c.75.6 1.2 1.3 1.3 2.22h5.8c.1-.92.55-1.62 1.3-2.22A6.75 6.75 0 0 0 12 2.75Z" }), /* @__PURE__ */ u("path", { d: "M9.25 19h5.5M10 21.25h4" })] }),
	settings: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("circle", {
		cx: "12",
		cy: "12",
		r: "3"
	}), /* @__PURE__ */ u("path", { d: "M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.86 2.86-.06-.06A1.7 1.7 0 0 0 15 19.4a1.7 1.7 0 0 0-1 .6 1.7 1.7 0 0 0-.4 1.1V21h-4v-.1A1.7 1.7 0 0 0 8.6 19.4a1.7 1.7 0 0 0-1.88.34l-.06.06-2.86-2.86.06-.06A1.7 1.7 0 0 0 4.2 15a1.7 1.7 0 0 0-.6-1 1.7 1.7 0 0 0-1.1-.4H2.4v-4h.1A1.7 1.7 0 0 0 4.2 8.6a1.7 1.7 0 0 0-.34-1.88l-.06-.06L6.66 3.8l.06.06A1.7 1.7 0 0 0 8.6 4.2a1.7 1.7 0 0 0 1-.6 1.7 1.7 0 0 0 .4-1.1v-.1h4v.1a1.7 1.7 0 0 0 1 1.7 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.86 2.86-.06.06A1.7 1.7 0 0 0 19.4 8.6a1.7 1.7 0 0 0 .6 1 1.7 1.7 0 0 0 1.1.4h.1v4h-.1a1.7 1.7 0 0 0-1.7 1Z" })] }),
	diagnostics: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("circle", {
		cx: "12",
		cy: "12",
		r: "9"
	}), /* @__PURE__ */ u("path", { d: "M12 10v6M12 7h.01" })] }),
	about: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M5 3.5h10.5L19 7v13.5H5z" }), /* @__PURE__ */ u("path", { d: "M15.5 3.5V7H19M8.5 11h7M8.5 14h7M8.5 17h4" })] }),
	power: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M12 2.75v9" }), /* @__PURE__ */ u("path", { d: "M6.2 6.2a8 8 0 1 0 11.6 0" })] }),
	refresh: /* @__PURE__ */ u(S$1, { children: [
		/* @__PURE__ */ u("path", { d: "M20 11a8 8 0 0 0-14.9-4L3 10" }),
		/* @__PURE__ */ u("path", { d: "M3 4v6h6M4 13a8 8 0 0 0 14.9 4L21 14" }),
		/* @__PURE__ */ u("path", { d: "M21 20v-6h-6" })
	] }),
	reconnect: /* @__PURE__ */ u(S$1, { children: [
		/* @__PURE__ */ u("path", { d: "M19 8a8 8 0 0 0-13.5-2L3 9" }),
		/* @__PURE__ */ u("path", { d: "M3 4v5h5M5 16a8 8 0 0 0 13.5 2l2.5-3" }),
		/* @__PURE__ */ u("path", { d: "M21 20v-5h-5" })
	] }),
	window: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("rect", {
		x: "3",
		y: "4",
		width: "18",
		height: "16",
		rx: "2"
	}), /* @__PURE__ */ u("path", { d: "M3 8h18M6 6h.01M9 6h.01" })] }),
	moon: /* @__PURE__ */ u("path", { d: "M20 15.2A8.5 8.5 0 0 1 8.8 4a8.5 8.5 0 1 0 11.2 11.2Z" }),
	sun: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("circle", {
		cx: "12",
		cy: "12",
		r: "4"
	}), /* @__PURE__ */ u("path", { d: "M12 2v2M12 20v2M4.93 4.93l1.42 1.42M17.65 17.65l1.42 1.42M2 12h2M20 12h2M4.93 19.07l1.42-1.42M17.65 6.35l1.42-1.42" })] }),
	system: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("rect", {
		x: "3",
		y: "4",
		width: "18",
		height: "14",
		rx: "2"
	}), /* @__PURE__ */ u("path", { d: "M8 22h8M12 18v4" })] }),
	check: /* @__PURE__ */ u("path", { d: "m4 12 5 5L20 6" }),
	warning: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M10.3 3.7 2.4 18a2 2 0 0 0 1.75 3h15.7a2 2 0 0 0 1.75-3L13.7 3.7a2 2 0 0 0-3.4 0Z" }), /* @__PURE__ */ u("path", { d: "M12 9v4M12 17h.01" })] }),
	close: /* @__PURE__ */ u("path", { d: "m5 5 14 14M19 5 5 19" }),
	menu: /* @__PURE__ */ u("path", { d: "M4 7h16M4 12h16M4 17h16" }),
	chevron: /* @__PURE__ */ u("path", { d: "m9 5 7 7-7 7" }),
	bell: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9Z" }), /* @__PURE__ */ u("path", { d: "M10 21h4" })] }),
	sound: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M5 10v4h3l4 3V7l-4 3H5Z" }), /* @__PURE__ */ u("path", { d: "M15 9.2a4 4 0 0 1 0 5.6M17.8 6.4a8 8 0 0 1 0 11.2" })] }),
	upload: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M12 16V4M7.5 8.5 12 4l4.5 4.5" }), /* @__PURE__ */ u("path", { d: "M4 15v4.5h16V15" })] }),
	keyboard: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("rect", {
		x: "2.5",
		y: "5.5",
		width: "19",
		height: "13",
		rx: "2.25"
	}), /* @__PURE__ */ u("path", { d: "M6 9h.01M9 9h.01M12 9h.01M15 9h.01M18 9h.01M6 12.5h.01M9 12.5h.01M12 12.5h.01M15 12.5h3M7.5 16h9" })] }),
	terminal: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("rect", {
		x: "3",
		y: "4",
		width: "18",
		height: "16",
		rx: "2"
	}), /* @__PURE__ */ u("path", { d: "m7 9 3 3-3 3M12.5 16h4.5" })] }),
	network: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("path", { d: "M4.9 9a10 10 0 0 1 14.2 0M7.75 11.85a6 6 0 0 1 8.5 0M10.6 14.7a2 2 0 0 1 2.8 0" }), /* @__PURE__ */ u("circle", {
		cx: "12",
		cy: "18",
		r: ".75"
	})] }),
	info: /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u("circle", {
		cx: "12",
		cy: "12",
		r: "9"
	}), /* @__PURE__ */ u("path", { d: "M12 11v6M12 7h.01" })] })
};
function Icon({ name, size = 20, label, class: className }) {
	return /* @__PURE__ */ u("svg", {
		class: className,
		width: size,
		height: size,
		viewBox: "0 0 24 24",
		fill: "none",
		stroke: "currentColor",
		"stroke-width": "1.75",
		"stroke-linecap": "round",
		"stroke-linejoin": "round",
		"aria-hidden": label ? void 0 : "true",
		"aria-label": label,
		role: label ? "img" : void 0,
		children: paths[name]
	});
}
//#endregion
//#region src/components/controls.tsx
function Card({ children, class: className = "", as: Tag = "section", ...props }) {
	return /* @__PURE__ */ u(Tag, {
		class: `glass-card ${className}`,
		...props,
		children
	});
}
function FieldGroup({ title, description, children }) {
	return /* @__PURE__ */ u("fieldset", {
		class: "field-group",
		children: [
			/* @__PURE__ */ u("legend", { children: t(title) }),
			description && /* @__PURE__ */ u("p", {
				class: "field-group-description",
				children: t(description)
			}),
			/* @__PURE__ */ u("div", {
				class: "field-stack",
				children
			})
		]
	});
}
function Toggle({ label, description, checked, disabled, onChange }) {
	return /* @__PURE__ */ u("label", {
		class: `toggle-row${disabled ? " is-disabled" : ""}`,
		children: [
			/* @__PURE__ */ u("span", {
				class: "field-copy",
				children: [/* @__PURE__ */ u("span", {
					class: "field-label",
					children: t(label)
				}), description && /* @__PURE__ */ u("span", {
					class: "field-description",
					children: t(description)
				})]
			}),
			/* @__PURE__ */ u("input", {
				class: "toggle-input",
				type: "checkbox",
				checked,
				disabled,
				onChange: (event) => onChange(event.currentTarget.checked)
			}),
			/* @__PURE__ */ u("span", {
				class: "toggle-track",
				"aria-hidden": "true",
				children: /* @__PURE__ */ u("span", { class: "toggle-thumb" })
			})
		]
	});
}
function TextField({ label, description, value, placeholder, type = "text", readOnly, autoComplete, dir, onInput }) {
	const id = fieldID(label);
	return /* @__PURE__ */ u("label", {
		class: "input-field",
		for: id,
		children: [
			/* @__PURE__ */ u("span", {
				class: "field-label",
				children: t(label)
			}),
			description && /* @__PURE__ */ u("span", {
				class: "field-description",
				children: t(description)
			}),
			/* @__PURE__ */ u("input", {
				id,
				type,
				value,
				placeholder,
				readOnly,
				autocomplete: autoComplete,
				dir: dir ?? (type === "url" || type === "password" ? "ltr" : void 0),
				onInput: (event) => onInput?.(event.currentTarget.value)
			})
		]
	});
}
function NumberField({ label, description, value, min, max, step = 1, onInput }) {
	const id = fieldID(label);
	return /* @__PURE__ */ u("label", {
		class: "input-field",
		for: id,
		children: [
			/* @__PURE__ */ u("span", {
				class: "field-label",
				children: t(label)
			}),
			description && /* @__PURE__ */ u("span", {
				class: "field-description",
				children: t(description)
			}),
			/* @__PURE__ */ u("input", {
				id,
				type: "number",
				value: String(value),
				min,
				max,
				step,
				inputMode: "decimal",
				onInput: (event) => {
					const next = event.currentTarget.valueAsNumber;
					if (Number.isFinite(next)) onInput(next);
				}
			})
		]
	});
}
function SelectField({ label, description, value, options, onChange }) {
	const id = fieldID(label);
	return /* @__PURE__ */ u("label", {
		class: "input-field",
		for: id,
		children: [
			/* @__PURE__ */ u("span", {
				class: "field-label",
				children: t(label)
			}),
			description && /* @__PURE__ */ u("span", {
				class: "field-description",
				children: t(description)
			}),
			/* @__PURE__ */ u("span", {
				class: "select-wrap",
				children: /* @__PURE__ */ u("select", {
					id,
					value,
					onChange: (event) => onChange(event.currentTarget.value),
					children: options.map((option) => /* @__PURE__ */ u("option", {
						value: option.value,
						children: t(option.label)
					}, option.value))
				})
			})
		]
	});
}
function TextAreaField({ label, description, value, rows = 4, spellcheck = false, onInput }) {
	const id = fieldID(label);
	return /* @__PURE__ */ u("label", {
		class: "input-field",
		for: id,
		children: [
			/* @__PURE__ */ u("span", {
				class: "field-label",
				children: t(label)
			}),
			description && /* @__PURE__ */ u("span", {
				class: "field-description",
				children: t(description)
			}),
			/* @__PURE__ */ u("textarea", {
				id,
				value,
				rows,
				spellcheck,
				onInput: (event) => onInput(event.currentTarget.value)
			})
		]
	});
}
function Button({ children, kind = "secondary", busy, icon, class: className = "", ...props }) {
	return /* @__PURE__ */ u("button", {
		class: `button button-${kind}${busy ? " is-busy" : ""} ${className}`,
		"aria-busy": busy || void 0,
		...props,
		children: [
			busy && /* @__PURE__ */ u("span", {
				class: "button-spinner",
				"aria-hidden": "true"
			}),
			!busy && icon,
			/* @__PURE__ */ u("span", { children: typeof children === "string" ? t(children) : children })
		]
	});
}
var nextFieldID = 0;
function fieldID(label) {
	return `field-${label.toLowerCase().replace(/[^a-z0-9]+/g, "-")}-${++nextFieldID}`;
}
//#endregion
//#region src/components/modal.tsx
function Modal({ open, title, description, children, onClose }) {
	const dialog = A(null);
	const identity = g$2();
	const titleID = `${identity}-title`;
	const descriptionID = `${identity}-description`;
	h$2(() => {
		const element = dialog.current;
		if (!element) return;
		if (open && !element.open) element.showModal();
		if (!open && element.open) element.close();
	}, [open]);
	return /* @__PURE__ */ u("dialog", {
		ref: dialog,
		class: "modal",
		"aria-labelledby": titleID,
		"aria-describedby": description ? descriptionID : void 0,
		onCancel: (event) => {
			event.preventDefault();
			onClose();
		},
		onClose,
		onClick: (event) => {
			if (event.target === dialog.current) onClose();
		},
		children: /* @__PURE__ */ u("div", {
			class: "modal-surface",
			children: [/* @__PURE__ */ u("header", {
				class: "modal-header",
				children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("h2", {
					id: titleID,
					children: title
				}), description && /* @__PURE__ */ u("p", {
					id: descriptionID,
					children: description
				})] }), /* @__PURE__ */ u("button", {
					class: "icon-button",
					type: "button",
					onClick: onClose,
					"aria-label": t("Close dialog"),
					children: /* @__PURE__ */ u(Icon, { name: "close" })
				})]
			}), /* @__PURE__ */ u("div", {
				class: "modal-content",
				children
			})]
		})
	});
}
//#endregion
//#region src/components/connection-dialog.tsx
function ConnectionDialog({ open, preferences, activeKind, activeLabel, onClose, onApply }) {
	const [mode, setMode] = d$2(preferences.mode);
	const [lampURL, setLampURL] = d$2(preferences.lampURL);
	const [error, setError] = d$2("");
	h$2(() => {
		if (!open) return;
		setMode(preferences.mode);
		setLampURL(preferences.lampURL);
		setError("");
	}, [
		open,
		preferences.mode,
		preferences.lampURL
	]);
	const apply = () => {
		try {
			onApply({
				mode,
				lampURL: normalizeLampBaseURL(lampURL)
			});
			onClose();
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : t("Enter a valid lamp address."));
		}
	};
	return /* @__PURE__ */ u(Modal, {
		open,
		title: t("Connection mode"),
		description: t("Choose who owns lamp communication. This preference is shared with other dashboard tabs."),
		onClose,
		children: [/* @__PURE__ */ u("div", {
			class: "connection-dialog",
			children: [
				/* @__PURE__ */ u("div", {
					class: "active-transport",
					role: "status",
					children: [/* @__PURE__ */ u("span", {
						class: `transport-glyph transport-${activeKind}`,
						children: /* @__PURE__ */ u(Icon, { name: activeKind === "direct" ? "lamp" : "network" })
					}), /* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u("small", { children: t("Active now") }), /* @__PURE__ */ u("strong", { children: t(activeLabel) })] })]
				}),
				/* @__PURE__ */ u(SelectField, {
					label: t("Preferred transport"),
					value: mode,
					options: [
						{
							value: "auto",
							label: t("Automatic — coordinator, then direct")
						},
						{
							value: "relay",
							label: t("Go coordinator")
						},
						{
							value: "direct",
							label: t("Direct to lamp")
						}
					],
					onChange: (value) => setMode(value)
				}),
				/* @__PURE__ */ u("div", {
					class: "transport-explainer",
					children: [/* @__PURE__ */ u("div", {
						class: mode === "relay" ? "is-selected" : "",
						children: [/* @__PURE__ */ u(Icon, { name: "network" }), /* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u("strong", { children: t("Go coordinator") }), /* @__PURE__ */ u("small", { children: t("Uses the same-origin RayanLamp manager for resilient discovery, settings, diagnostics, automation, and native integration.") })] })]
					}), /* @__PURE__ */ u("div", {
						class: mode === "direct" ? "is-selected" : "",
						children: [/* @__PURE__ */ u(Icon, { name: "lamp" }), /* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u("strong", { children: t("Direct browser connection") }), /* @__PURE__ */ u("small", { children: t("Uses firmware HTTP and WebSocket endpoints. Native-only features are intentionally hidden.") })] })]
					})]
				}),
				(mode === "direct" || mode === "auto") && /* @__PURE__ */ u(TextField, {
					label: t("Lamp address"),
					type: "url",
					value: lampURL,
					placeholder: "http://rayanlamp.local",
					description: t("HTTP or HTTPS origin only; no path, query, fragment, or credentials."),
					onInput: setLampURL
				}),
				isStaticExport() && mode === "relay" && /* @__PURE__ */ u("p", {
					class: "connection-note",
					children: [/* @__PURE__ */ u(Icon, {
						name: "info",
						size: 16
					}), t("The static edition cannot borrow the manager’s private browser session cross-origin. Open the dashboard served by RayanLamp.exe to use coordinator mode.")]
				}),
				(mode === "direct" || mode === "auto") && /* @__PURE__ */ u("p", {
					class: "connection-note",
					children: [/* @__PURE__ */ u(Icon, {
						name: "info",
						size: 16
					}), t("Serve the ZIP over HTTP. An HTTPS page cannot control the current HTTP/WS lamp because browsers block mixed content.")]
				}),
				error && /* @__PURE__ */ u("p", {
					class: "field-error",
					role: "alert",
					children: t(error)
				})
			]
		}), /* @__PURE__ */ u("div", {
			class: "modal-actions",
			children: [/* @__PURE__ */ u(Button, {
				kind: "quiet",
				onClick: onClose,
				children: t("Cancel")
			}), /* @__PURE__ */ u(Button, {
				kind: "primary",
				onClick: apply,
				children: t("Apply and reconnect")
			})]
		})]
	});
}
//#endregion
//#region src/components/toasts.tsx
function ToastRegion() {
	return /* @__PURE__ */ u("div", {
		class: "toast-region",
		"aria-live": "polite",
		"aria-atomic": "false",
		children: toasts.value.map((toast) => /* @__PURE__ */ u("div", {
			class: `app-toast toast-${toast.tone}`,
			role: "status",
			children: [
				/* @__PURE__ */ u("span", {
					class: "toast-icon",
					children: /* @__PURE__ */ u(Icon, { name: toast.tone === "error" ? "warning" : toast.tone === "success" ? "check" : "info" })
				}),
				/* @__PURE__ */ u("span", {
					class: "toast-copy",
					children: [/* @__PURE__ */ u("strong", { children: toast.title }), toast.message && /* @__PURE__ */ u("span", { children: toast.message })]
				}),
				/* @__PURE__ */ u("button", {
					type: "button",
					class: "icon-button",
					"aria-label": t("Dismiss {title}", { title: toast.title }),
					onClick: () => dismissToast(toast.id),
					children: /* @__PURE__ */ u(Icon, {
						name: "close",
						size: 16
					})
				})
			]
		}, toast.id))
	});
}
//#endregion
//#region src/components/lamp-visual.tsx
function LampVisual({ state, offline, busy }) {
	return /* @__PURE__ */ u("div", {
		class: `lamp-visual lamp-${offline ? "offline" : state}${busy ? " is-busy" : ""}`,
		role: "img",
		"aria-label": offline ? t("Lamp is offline") : state === "on" ? t("Lamp is on") : state === "off" ? t("Lamp is off") : t("Lamp state is unknown"),
		children: [
			/* @__PURE__ */ u("div", { class: "lamp-aura" }),
			/* @__PURE__ */ u("svg", {
				viewBox: "0 0 512 512",
				focusable: "false",
				"aria-hidden": "true",
				children: [
					/* @__PURE__ */ u("defs", { children: [
						/* @__PURE__ */ u("linearGradient", {
							id: "web-lamp-glass",
							x1: "0",
							y1: "0",
							x2: ".78",
							y2: "1",
							children: [
								/* @__PURE__ */ u("stop", {
									class: "lamp-stop-top",
									offset: "0"
								}),
								/* @__PURE__ */ u("stop", {
									class: "lamp-stop-core",
									offset: ".56"
								}),
								/* @__PURE__ */ u("stop", {
									class: "lamp-stop-bottom",
									offset: "1"
								})
							]
						}),
						/* @__PURE__ */ u("linearGradient", {
							id: "web-lamp-base",
							x1: "0",
							y1: "0",
							x2: "0",
							y2: "1",
							children: [
								/* @__PURE__ */ u("stop", {
									offset: "0",
									"stop-color": "#7d8c9b"
								}),
								/* @__PURE__ */ u("stop", {
									offset: ".45",
									"stop-color": "#465462"
								}),
								/* @__PURE__ */ u("stop", {
									offset: "1",
									"stop-color": "#202b36"
								})
							]
						}),
						/* @__PURE__ */ u("filter", {
							id: "web-lamp-glow",
							x: "-80%",
							y: "-80%",
							width: "260%",
							height: "260%",
							children: /* @__PURE__ */ u("feGaussianBlur", { stdDeviation: "15" })
						})
					] }),
					/* @__PURE__ */ u("g", {
						class: "lamp-rays",
						children: [
							/* @__PURE__ */ u("path", { d: "M256 15v35" }),
							/* @__PURE__ */ u("path", { d: "m124 57 25 31" }),
							/* @__PURE__ */ u("path", { d: "m50 173 41 8" }),
							/* @__PURE__ */ u("path", { d: "m388 57-25 31" }),
							/* @__PURE__ */ u("path", { d: "m462 173-41 8" }),
							/* @__PURE__ */ u("path", { d: "m70 305 37-17" }),
							/* @__PURE__ */ u("path", { d: "m442 305-37-17" })
						]
					}),
					/* @__PURE__ */ u("path", {
						class: "lamp-glow",
						d: "M256 66c-79 0-136 60-136 136 0 55 29 89 57 117 17 17 24 35 25 57h108c1-22 8-40 25-57 28-28 57-62 57-117 0-76-57-136-136-136Z",
						filter: "url(#web-lamp-glow)"
					}),
					/* @__PURE__ */ u("path", {
						class: "lamp-glass",
						d: "M256 66c-79 0-136 60-136 136 0 55 29 89 57 117 17 17 24 35 25 57h108c1-22 8-40 25-57 28-28 57-62 57-117 0-76-57-136-136-136Z",
						fill: "url(#web-lamp-glass)"
					}),
					/* @__PURE__ */ u("path", {
						class: "lamp-shine",
						d: "M185 151c15-31 42-51 77-55"
					}),
					/* @__PURE__ */ u("path", {
						class: "lamp-filament",
						d: "m218 281 38-49 38 49M256 232v98"
					}),
					/* @__PURE__ */ u("path", {
						class: "lamp-base",
						d: "M197 369h118v30c0 10-8 18-18 18h-82c-10 0-18-8-18-18Z",
						fill: "url(#web-lamp-base)"
					}),
					/* @__PURE__ */ u("path", {
						class: "lamp-base-lines",
						d: "M205 390h102M209 407h94"
					}),
					/* @__PURE__ */ u("path", {
						class: "lamp-foot",
						d: "M222 418h68v10c0 22-15 38-34 38s-34-16-34-38Z"
					})
				]
			}),
			offline && /* @__PURE__ */ u("span", {
				class: "offline-badge",
				"aria-hidden": "true",
				children: /* @__PURE__ */ u("span", {})
			})
		]
	});
}
//#endregion
//#region src/views/dashboard.tsx
function Dashboard({ api, onSnapshot = applySnapshot }) {
	const [reconnectOpen, setReconnectOpen] = d$2(false);
	const current = snapshot.value;
	const device = diagnostics.value?.device;
	const displayName = device?.device_name || device?.description?.friendly_name || t("Rayan Lamp");
	const busy = Boolean(commandPending.value || current.pending_command);
	const run = async (command, success) => {
		if (commandPending.value) return;
		commandPending.value = command;
		webHaptics.trigger("command.start");
		const commandStarted = performance.now();
		browserConsole.event("info", "lamp.command", `Sending ${command.replace("/", " ")} command`, {
			source: api.kind,
			action: command
		});
		try {
			await api.command(command);
			const next = await api.status();
			onSnapshot(next);
			browserConsole.event("success", "lamp.command", `${command.replace("/", " ")} command completed`, {
				source: api.kind,
				action: command,
				state: next.state,
				elapsed_ms: Math.round(performance.now() - commandStarted)
			});
			webHaptics.trigger("command.success");
			if (success) showToast("success", success);
		} catch (error) {
			browserConsole.event("error", "lamp.command", `${command.replace("/", " ")} command failed`, {
				source: api.kind,
				action: command,
				elapsed_ms: Math.round(performance.now() - commandStarted),
				...browserErrorMetadata(error)
			});
			webHaptics.trigger("command.error");
			showToast("error", t("Command failed"), error instanceof Error ? t(error.message) : t("The lamp did not respond."));
		} finally {
			commandPending.value = "";
		}
	};
	return /* @__PURE__ */ u("div", {
		class: "view dashboard-view",
		children: [
			/* @__PURE__ */ u("header", {
				class: "view-heading",
				children: [/* @__PURE__ */ u("div", { children: [
					/* @__PURE__ */ u("span", {
						class: "eyebrow",
						children: t("Office lighting")
					}),
					/* @__PURE__ */ u("h1", { children: displayName }),
					/* @__PURE__ */ u("p", { children: t("Local, private control with a resilient live connection.") })
				] }), /* @__PURE__ */ u("span", {
					class: `phase-pill phase-${appPhase.value}`,
					children: [/* @__PURE__ */ u("span", {
						class: "phase-dot",
						"aria-hidden": "true"
					}), phaseLabel(appPhase.value)]
				})]
			}),
			/* @__PURE__ */ u("div", {
				class: "dashboard-grid",
				children: [/* @__PURE__ */ u(Card, {
					class: "lamp-hero",
					children: [
						/* @__PURE__ */ u("div", {
							class: "lamp-hero-copy",
							children: [
								/* @__PURE__ */ u("span", {
									class: "eyebrow",
									children: device?.hostname || current.device_url ? /* @__PURE__ */ u(BidiLtr, { children: device?.hostname || current.device_url }) : t("Configured lamp")
								}),
								/* @__PURE__ */ u("h2", { children: stateLabel.value }),
								/* @__PURE__ */ u("p", { children: stateDetail(current.last_error, current.pending_command) })
							]
						}),
						/* @__PURE__ */ u("button", {
							type: "button",
							class: "lamp-toggle",
							"aria-label": current.state === "on" ? t("Turn lamp off") : t("Turn lamp on"),
							disabled: !isOnline.value || busy,
							onClick: () => void run("toggle"),
							children: /* @__PURE__ */ u(LampVisual, {
								state: current.state,
								offline: !isOnline.value,
								busy
							})
						}),
						/* @__PURE__ */ u("div", {
							class: "hero-actions",
							"aria-label": t("Lamp controls"),
							children: [
								/* @__PURE__ */ u(Button, {
									kind: "primary",
									icon: /* @__PURE__ */ u(Icon, { name: "power" }),
									disabled: !isOnline.value || busy,
									busy: commandPending.value === "toggle",
									onClick: () => void run("toggle"),
									children: t("Toggle lamp")
								}),
								/* @__PURE__ */ u(Button, {
									disabled: !isOnline.value || busy || current.state === "on",
									busy: commandPending.value === "on",
									onClick: () => void run("on"),
									children: t("Turn on")
								}),
								/* @__PURE__ */ u(Button, {
									disabled: !isOnline.value || busy || current.state === "off",
									busy: commandPending.value === "off",
									onClick: () => void run("off"),
									children: t("Turn off")
								})
							]
						})
					]
				}), /* @__PURE__ */ u("div", {
					class: "dashboard-side",
					children: [/* @__PURE__ */ u(Card, {
						class: "status-card",
						"aria-labelledby": "connection-heading",
						children: [
							/* @__PURE__ */ u("div", {
								class: "card-heading-row",
								children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("span", {
									class: "eyebrow",
									children: t("Connection")
								}), /* @__PURE__ */ u("h2", {
									id: "connection-heading",
									children: t("Device channels")
								})] }), /* @__PURE__ */ u(Icon, {
									name: "network",
									size: 24
								})]
							}),
							/* @__PURE__ */ u("dl", {
								class: "status-list",
								children: [
									/* @__PURE__ */ u(StatusRow, {
										label: t("HTTP control"),
										active: current.http_reachable,
										value: current.http_reachable ? t("Available") : t("Unavailable")
									}),
									/* @__PURE__ */ u(StatusRow, {
										label: t("WebSocket events"),
										active: current.websocket_online,
										value: current.websocket_online ? t("Connected") : t("Disconnected")
									}),
									/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Last source") }), /* @__PURE__ */ u("dd", { children: current.last_source ? /* @__PURE__ */ u(BidiLtr, { children: current.last_source }) : t("Waiting for an event") })] }),
									/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Last change") }), /* @__PURE__ */ u("dd", { children: formatTime(current.last_changed) })] }),
									device?.firmware && /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Firmware") }), /* @__PURE__ */ u("dd", { children: /* @__PURE__ */ u(BidiLtr, { children: device.firmware }) })] }),
									(device?.signal_quality || device?.rssi_dbm !== void 0) && /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Wi-Fi signal") }), /* @__PURE__ */ u("dd", { children: device.signal_quality || `${formatNumber(device.rssi_dbm ?? 0)} dBm` })] })
								]
							}),
							/* @__PURE__ */ u("div", {
								class: "card-actions",
								children: [/* @__PURE__ */ u(Button, {
									kind: "quiet",
									icon: /* @__PURE__ */ u(Icon, { name: "refresh" }),
									busy: commandPending.value === "refresh",
									disabled: busy,
									onClick: () => void run("refresh", t("Lamp state refreshed")),
									children: t("Refresh")
								}), /* @__PURE__ */ u(Button, {
									kind: "quiet",
									icon: /* @__PURE__ */ u(Icon, { name: "reconnect" }),
									disabled: busy,
									onClick: () => setReconnectOpen(true),
									children: t("Reconnect")
								})]
							})
						]
					}), capabilities.value.window_control !== false && /* @__PURE__ */ u(Card, {
						class: "quick-card",
						"aria-labelledby": "window-heading",
						children: [
							/* @__PURE__ */ u("div", {
								class: "card-heading-row",
								children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("span", {
									class: "eyebrow",
									children: t("Desktop app")
								}), /* @__PURE__ */ u("h2", {
									id: "window-heading",
									children: t("Native window")
								})] }), /* @__PURE__ */ u(Icon, {
									name: "window",
									size: 24
								})]
							}),
							/* @__PURE__ */ u("p", { children: t("Bring the compact controller forward or tuck it back into the notification area.") }),
							/* @__PURE__ */ u("div", {
								class: "card-actions",
								children: [/* @__PURE__ */ u(Button, {
									onClick: () => void run("window/show", t("Native window opened")),
									children: t("Show")
								}), /* @__PURE__ */ u(Button, {
									kind: "quiet",
									onClick: () => void run("window/hide", t("Native window hidden")),
									children: t("Hide")
								})]
							})
						]
					})]
				})]
			}),
			/* @__PURE__ */ u(Modal, {
				open: reconnectOpen,
				title: t("Reconnect to Rayan Lamp?"),
				description: t("The current HTTP and WebSocket transports will restart. The cached .local address is retained unless the connection fails."),
				onClose: () => setReconnectOpen(false),
				children: /* @__PURE__ */ u("div", {
					class: "modal-actions",
					children: [/* @__PURE__ */ u(Button, {
						kind: "quiet",
						onClick: () => setReconnectOpen(false),
						children: t("Keep connection")
					}), /* @__PURE__ */ u(Button, {
						kind: "primary",
						icon: /* @__PURE__ */ u(Icon, { name: "reconnect" }),
						onClick: () => {
							setReconnectOpen(false);
							run("reconnect", t("Reconnection started"));
						},
						children: t("Reconnect")
					})]
				})
			})
		]
	});
}
function StatusRow({ label, active, value }) {
	return /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: label }), /* @__PURE__ */ u("dd", { children: /* @__PURE__ */ u("span", {
		class: `mini-status ${active ? "is-active" : "is-inactive"}`,
		children: [/* @__PURE__ */ u("span", { "aria-hidden": "true" }), value]
	}) })] });
}
function phaseLabel(phase) {
	switch (phase) {
		case "online": return t("Connected");
		case "degraded": return t("Partially connected");
		case "connecting": return t("Reconnecting");
		case "offline": return t("Unavailable");
		default: return t("Starting");
	}
}
function stateDetail(lastError, pending) {
	if (pending) return t("Applying {command}…", { command: pending.replace("lamp.", "") });
	if (lastError) return t(lastError);
	return t("Press the lamp or use an explicit action below.");
}
function formatTime(value) {
	if (!value) return t("Not reported yet");
	const parsed = new Date(value);
	if (Number.isNaN(parsed.valueOf())) return value;
	return formatDateTime(parsed, {
		dateStyle: "medium",
		timeStyle: "short"
	});
}
//#endregion
//#region src/components/hotkey-recorder.tsx
var modifierOrder = [
	"Ctrl",
	"Alt",
	"Shift",
	"Win"
];
var modifierAliases = {
	ctrl: "Ctrl",
	control: "Ctrl",
	alt: "Alt",
	shift: "Shift",
	win: "Win",
	windows: "Win",
	meta: "Win",
	super: "Win"
};
var namedKeys = {
	backspace: "Backspace",
	back: "Backspace",
	tab: "Tab",
	enter: "Enter",
	return: "Enter",
	pause: "Pause",
	capslock: "CapsLock",
	escape: "Escape",
	esc: "Escape",
	space: "Space",
	pageup: "PageUp",
	pgup: "PageUp",
	prior: "PageUp",
	pagedown: "PageDown",
	pgdn: "PageDown",
	next: "PageDown",
	end: "End",
	home: "Home",
	left: "Left",
	up: "Up",
	right: "Right",
	down: "Down",
	printscreen: "PrintScreen",
	print_screen: "PrintScreen",
	snapshot: "PrintScreen",
	prtsc: "PrintScreen",
	prtscr: "PrintScreen",
	insert: "Insert",
	ins: "Insert",
	delete: "Delete",
	del: "Delete",
	apps: "Apps",
	menu: "Apps",
	contextmenu: "Apps",
	context_menu: "Apps",
	application: "Apps",
	sleep: "Sleep",
	multiply: "Multiply",
	add: "Add",
	separator: "Separator",
	numseparator: "Separator",
	numpadseparator: "Separator",
	subtract: "Subtract",
	decimal: "Decimal",
	divide: "Divide",
	numlock: "NumLock",
	scrolllock: "ScrollLock",
	browserback: "BrowserBack",
	browser_back: "BrowserBack",
	browserforward: "BrowserForward",
	browser_forward: "BrowserForward",
	browserrefresh: "BrowserRefresh",
	browser_refresh: "BrowserRefresh",
	browserstop: "BrowserStop",
	browser_stop: "BrowserStop",
	browsersearch: "BrowserSearch",
	browser_search: "BrowserSearch",
	browserfavorites: "BrowserFavorites",
	browser_favorites: "BrowserFavorites",
	browserhome: "BrowserHome",
	browser_home: "BrowserHome",
	volumemute: "VolumeMute",
	volume_mute: "VolumeMute",
	volumedown: "VolumeDown",
	volume_down: "VolumeDown",
	volumeup: "VolumeUp",
	volume_up: "VolumeUp",
	medianext: "MediaNext",
	media_next: "MediaNext",
	mediaprevious: "MediaPrevious",
	media_previous: "MediaPrevious",
	mediastop: "MediaStop",
	media_stop: "MediaStop",
	mediaplaypause: "MediaPlayPause",
	media_play_pause: "MediaPlayPause",
	launchmail: "LaunchMail",
	launch_mail: "LaunchMail",
	mail: "LaunchMail",
	launchmedia: "LaunchMediaSelect",
	launch_media: "LaunchMediaSelect",
	launchmediaselect: "LaunchMediaSelect",
	media_select: "LaunchMediaSelect",
	launchapp1: "LaunchApp1",
	launch_app1: "LaunchApp1",
	app1: "LaunchApp1",
	launchapp2: "LaunchApp2",
	launch_app2: "LaunchApp2",
	app2: "LaunchApp2",
	semicolon: "Semicolon",
	";": "Semicolon",
	equal: "Equal",
	equals: "Equal",
	"=": "Equal",
	comma: "Comma",
	",": "Comma",
	minus: "Minus",
	"-": "Minus",
	period: "Period",
	dot: "Period",
	".": "Period",
	slash: "Slash",
	"/": "Slash",
	backtick: "Backtick",
	grave: "Backtick",
	"`": "Backtick",
	leftbracket: "LeftBracket",
	left_bracket: "LeftBracket",
	"[": "LeftBracket",
	backslash: "Backslash",
	"\\": "Backslash",
	rightbracket: "RightBracket",
	right_bracket: "RightBracket",
	"]": "RightBracket",
	quote: "Quote",
	apostrophe: "Quote",
	"'": "Quote"
};
for (let number = 0; number <= 9; number += 1) {
	namedKeys[`num${number}`] = `Num${number}`;
	namedKeys[`numpad${number}`] = `Num${number}`;
}
function canonicalizeHotkey(value) {
	const trimmed = value.trim();
	if (!trimmed) return "";
	const modifiers = /* @__PURE__ */ new Set();
	let key = "";
	for (const rawToken of trimmed.split("+")) {
		const token = rawToken.trim();
		if (!token) return null;
		const modifier = modifierAliases[token.toLowerCase()];
		if (modifier) {
			if (modifiers.has(modifier)) return null;
			modifiers.add(modifier);
			continue;
		}
		if (key) return null;
		key = canonicalKeyName(token) ?? "";
		if (!key) return null;
	}
	if (!key) return null;
	return [...modifierOrder.filter((modifier) => modifiers.has(modifier)), key].join("+");
}
function HotkeyRecorder({ label, value, conflicts = [], captureActive, captureError = "", onCaptureStart, onCaptureStop, onProbe, onCommit }) {
	const identity = g$2();
	const statusID = `${identity}-status`;
	const errorID = `${identity}-error`;
	const warningID = `${identity}-warning`;
	const [recording, setRecording] = d$2(false);
	const [heldModifiers, setHeldModifiers] = d$2([]);
	const [recordingError, setRecordingError] = d$2("");
	const [sessionError, setSessionError] = d$2("");
	const [statusNotice, setStatusNotice] = d$2("");
	const [preparing, setPreparing] = d$2(false);
	const [operation, setOperation] = d$2("");
	const normalizedValue = canonicalizeHotkey(value);
	const controlledIdentity = normalizedValue === null ? value.trim() : normalizedValue;
	const controlledIdentityRef = A(controlledIdentity);
	controlledIdentityRef.current = controlledIdentity;
	const captureBaseline = A(null);
	const expectedCommit = A(null);
	const previousControlledIdentity = A(controlledIdentity);
	const requestGeneration = A(0);
	const storedError = normalizedValue === null ? t("This stored shortcut is not supported. Record a replacement or clear it.") : reservedHotkeyMessage(normalizedValue) ? t("F12 is reserved by Windows. Record a different shortcut or clear it.") : conflictMessage(normalizedValue, conflicts);
	const storedWarning = !storedError && normalizedValue ? riskyHotkeyMessage(normalizedValue) : "";
	const lifecycleError = sessionError || captureError;
	const error = recording ? recordingError || lifecycleError : lifecycleError || storedError;
	const warning = recording || error ? "" : storedWarning;
	const displayedTokens = recording ? heldModifiers : normalizedValue === null ? value.trim() ? [value.trim()] : [] : normalizedValue ? normalizedValue.split("+") : [];
	const stopRecording = (notice = "") => {
		setRecording(false);
		setPreparing(false);
		setOperation("");
		setHeldModifiers([]);
		setRecordingError("");
		setStatusNotice(notice);
		captureBaseline.current = null;
	};
	const endCapture = async (reason) => {
		if (!onCaptureStop) return;
		try {
			const message = await onCaptureStop(reason);
			if (message) setSessionError(message);
		} catch (error) {
			setSessionError(recorderErrorMessage(error, "The capture session ended with a shortcut restore warning."));
		}
	};
	const cancelCapture = (notice = "") => {
		const wasCapturing = recording || preparing || operation !== "";
		requestGeneration.current += 1;
		stopRecording(notice);
		if (wasCapturing) endCapture("cancel");
	};
	const enterRecording = () => {
		captureBaseline.current = controlledIdentityRef.current;
		setPreparing(false);
		setRecording(true);
		setHeldModifiers([]);
		setRecordingError("");
		setSessionError("");
		setStatusNotice("");
	};
	const resolveCaptureStart = (generation, result) => {
		if (requestGeneration.current !== generation) return;
		setPreparing(false);
		if (!result.active) {
			if (result.message) setSessionError(result.message);
			return;
		}
		enterRecording();
	};
	const beginCapture = () => {
		if (recording) {
			cancelCapture();
			return;
		}
		const generation = ++requestGeneration.current;
		expectedCommit.current = null;
		setSessionError("");
		setRecordingError("");
		setStatusNotice("");
		if (!onCaptureStart) {
			enterRecording();
			return;
		}
		setPreparing(true);
		let result;
		try {
			result = onCaptureStart();
		} catch (error) {
			setPreparing(false);
			setSessionError(recorderErrorMessage(error, "Rayan Lamp could not prepare shortcut recording."));
			return;
		}
		if (isPromiseLike(result)) {
			result.then((value) => resolveCaptureStart(generation, value), (error) => {
				if (requestGeneration.current !== generation) return;
				setPreparing(false);
				setSessionError(recorderErrorMessage(error, "Rayan Lamp could not prepare shortcut recording."));
			});
			return;
		}
		resolveCaptureStart(generation, result);
	};
	const commitWithoutProbe = (chord) => {
		stopRecording();
		if (chord === controlledIdentityRef.current) return;
		expectedCommit.current = chord;
		try {
			const result = onCommit(chord);
			if (isPromiseLike(result)) result.catch((error) => {
				setSessionError(recorderErrorMessage(error, "The shortcut could not be applied."));
			});
		} catch (error) {
			setSessionError(recorderErrorMessage(error, "The shortcut could not be applied."));
		}
	};
	const applyCandidate = async (chord, probe) => {
		const generation = requestGeneration.current;
		setRecordingError("");
		setSessionError("");
		if (probe && onProbe) {
			setOperation("probing");
			let result;
			try {
				result = await onProbe(chord);
			} catch (error) {
				if (requestGeneration.current !== generation) return;
				setOperation("");
				setSessionError(recorderErrorMessage(error, "Rayan Lamp could not verify that shortcut. Try again."));
				return;
			}
			if (requestGeneration.current !== generation) return;
			if (!result.available) {
				setOperation("");
				setRecordingError(result.message || "Windows or another application already owns this shortcut.");
				return;
			}
		}
		if (requestGeneration.current !== generation) return;
		setOperation("applying");
		expectedCommit.current = chord;
		try {
			await onCommit(chord);
		} catch (error) {
			if (requestGeneration.current !== generation) return;
			setOperation("");
			setSessionError(recorderErrorMessage(error, "The shortcut could not be applied. Press it again to retry."));
			return;
		}
		if (requestGeneration.current !== generation) return;
		stopRecording();
		await endCapture("complete");
	};
	const clear = () => {
		if (recording && (onCaptureStart || onCaptureStop || onProbe)) {
			applyCandidate("", false);
			return;
		}
		const wasRecording = recording;
		if (value.trim()) expectedCommit.current = "";
		stopRecording();
		if (value.trim()) try {
			const result = onCommit("");
			if (isPromiseLike(result)) result.catch((error) => {
				setSessionError(recorderErrorMessage(error, "The shortcut could not be cleared."));
			});
		} catch (error) {
			setSessionError(recorderErrorMessage(error, "The shortcut could not be cleared."));
		}
		if (wasRecording) endCapture("complete");
	};
	h$2(() => {
		const previous = previousControlledIdentity.current;
		previousControlledIdentity.current = controlledIdentity;
		if (previous === controlledIdentity) return;
		if (expectedCommit.current !== null && expectedCommit.current === controlledIdentity) {
			expectedCommit.current = null;
			if (recording) captureBaseline.current = controlledIdentity;
			setStatusNotice("");
			return;
		}
		expectedCommit.current = null;
		if (recording && captureBaseline.current !== null && captureBaseline.current !== controlledIdentity) {
			const assignment = normalizedValue === null || storedError ? t("The new stored shortcut needs attention.") : normalizedValue ? t("{keys} is now assigned.", { keys: announceChord(normalizedValue) }) : t("The shortcut is now disabled.");
			cancelCapture(t("Shortcut changed elsewhere. Recording cancelled. {assignment}", { assignment }));
		} else setStatusNotice("");
	}, [
		controlledIdentity,
		normalizedValue,
		recording
	]);
	h$2(() => {
		if (captureActive !== false || !recording && !preparing && operation === "") return;
		requestGeneration.current += 1;
		stopRecording();
	}, [
		captureActive,
		operation,
		preparing,
		recording
	]);
	const handleKeyDown = (event) => {
		if (!recording || operation !== "") return;
		event.preventDefault();
		event.stopPropagation();
		if (event.repeat) return;
		if (event.key === "Escape" || event.key === "Esc") {
			cancelCapture();
			return;
		}
		const modifiers = modifiersFromEvent(event);
		if (modifiers.length === 0 && (event.key === "Backspace" || event.key === "Delete")) {
			clear();
			return;
		}
		if (isModifierKey(event.key)) {
			setHeldModifiers(modifiers);
			setRecordingError("");
			return;
		}
		const key = keyFromKeyboardEvent(event);
		if (!key) {
			setHeldModifiers(modifiers);
			setRecordingError(t("“{key}” is not supported for a global shortcut.", { key: event.key || event.code || t("That key") }));
			return;
		}
		const chord = [...modifiers, key].join("+");
		if (key === "F12") {
			setHeldModifiers(modifiers);
			setRecordingError(t("F12 is reserved by Windows. Choose another shortcut."));
			return;
		}
		const conflict = conflictMessage(chord, conflicts);
		if (conflict) {
			setHeldModifiers(modifiers);
			setRecordingError(conflict);
			return;
		}
		if (onCaptureStart || onCaptureStop || onProbe) applyCandidate(chord, Boolean(onProbe));
		else {
			commitWithoutProbe(chord);
			if (onCaptureStop) endCapture("complete");
		}
	};
	const handleKeyUp = (event) => {
		if (!recording || operation !== "") return;
		event.preventDefault();
		event.stopPropagation();
		if (isModifierKey(event.key)) setHeldModifiers(modifiersFromEvent(event));
	};
	const busy = preparing || operation !== "";
	const captureEngaged = recording || preparing;
	const statusText = preparing ? t("Preparing secure shortcut capture…") : operation === "probing" ? t("Checking whether Windows can register this shortcut…") : operation === "applying" ? t("Applying the shortcut before restoring global hotkeys…") : recording ? heldModifiers.length ? t("Holding {keys}. Press the main key; Escape cancels.", { keys: announceChord(heldModifiers.join("+")) }) : t("Waiting for a shortcut. Escape cancels; Backspace or Delete clears.") : (statusNotice ? t(statusNotice) : "") || (normalizedValue === null ? t("The stored shortcut needs attention.") : normalizedValue ? storedError ? t("{keys} is stored but unavailable.", { keys: announceChord(normalizedValue) }) : t("{keys} is assigned.", { keys: announceChord(normalizedValue) }) : t("No shortcut is assigned."));
	const messageID = error ? errorID : warning ? warningID : "";
	return /* @__PURE__ */ u("section", {
		class: `hotkey-recorder${captureEngaged ? " is-recording" : ""}${busy ? " is-busy" : ""}${error ? " has-error" : ""}${warning ? " has-warning" : ""}`,
		"aria-labelledby": `${identity}-label`,
		children: [
			/* @__PURE__ */ u("div", {
				class: "hotkey-recorder-heading",
				children: [/* @__PURE__ */ u("span", {
					id: `${identity}-label`,
					class: "field-label",
					children: t(label)
				}), /* @__PURE__ */ u("span", {
					class: "hotkey-recorder-kind",
					children: t("Global shortcut")
				})]
			}),
			/* @__PURE__ */ u("div", {
				class: "hotkey-recorder-value",
				dir: "ltr",
				"aria-hidden": "true",
				children: displayedTokens.length ? displayedTokens.map((token, index) => /* @__PURE__ */ u(S$1, { children: [index > 0 && /* @__PURE__ */ u("span", {
					class: "hotkey-plus",
					"aria-hidden": "true",
					children: "+"
				}), /* @__PURE__ */ u("kbd", {
					class: `hotkey-chip${recording ? " is-live" : ""}`,
					children: token
				})] }, `${token}-${index}`)) : /* @__PURE__ */ u("span", {
					class: "hotkey-empty",
					children: recording ? t("Press your shortcut…") : t("Not assigned")
				})
			}),
			/* @__PURE__ */ u("div", {
				class: "hotkey-recorder-actions",
				children: [/* @__PURE__ */ u(Button, {
					type: "button",
					kind: captureEngaged ? "primary" : "secondary",
					class: "hotkey-record-button",
					busy,
					disabled: busy,
					icon: recording ? /* @__PURE__ */ u("span", {
						class: "hotkey-recording-dot",
						"aria-hidden": "true"
					}) : /* @__PURE__ */ u(Icon, {
						name: "keyboard",
						size: 16
					}),
					"aria-label": preparing ? t("Preparing {label} shortcut recording", { label: t(label) }) : operation === "probing" ? t("Checking {label} shortcut availability", { label: t(label) }) : operation === "applying" ? t("Applying {label} shortcut", { label: t(label) }) : recording ? t("Cancel recording for {label}", { label: t(label) }) : t("Record {label} shortcut", { label: t(label) }),
					"aria-pressed": captureEngaged,
					"aria-describedby": `${statusID}${messageID ? ` ${messageID}` : ""}`,
					onClick: beginCapture,
					onKeyDown: handleKeyDown,
					onKeyUp: handleKeyUp,
					onBlur: (event) => {
						if (!recording || operation !== "") return;
						const next = event.relatedTarget;
						if (next instanceof Element) {
							const currentRecorder = event.currentTarget.closest(".hotkey-recorder");
							if (next.closest(".hotkey-recorder") === currentRecorder || next.closest(".hotkey-record-button")) return;
						}
						cancelCapture();
					},
					children: preparing ? t("Preparing") : operation === "probing" ? t("Checking") : operation === "applying" ? t("Applying") : recording ? t("Cancel") : t("Record")
				}), /* @__PURE__ */ u(Button, {
					type: "button",
					kind: "quiet",
					class: "hotkey-clear-button",
					disabled: busy || !value.trim() && !recording,
					"aria-label": t("Clear {label} shortcut", { label: t(label) }),
					onClick: clear,
					children: t("Clear")
				})]
			}),
			/* @__PURE__ */ u("output", {
				id: statusID,
				class: "hotkey-recorder-status",
				role: "status",
				"aria-live": "polite",
				"aria-atomic": "true",
				children: statusText
			}),
			error && /* @__PURE__ */ u("span", {
				id: errorID,
				class: "hotkey-recorder-error",
				role: "alert",
				children: [/* @__PURE__ */ u(Icon, {
					name: "warning",
					size: 14
				}), t(error)]
			}),
			warning && /* @__PURE__ */ u("span", {
				id: warningID,
				class: "hotkey-recorder-warning",
				role: "note",
				children: [/* @__PURE__ */ u(Icon, {
					name: "warning",
					size: 14
				}), t(warning)]
			})
		]
	});
}
function canonicalKeyName(token) {
	const upper = token.toUpperCase();
	if (/^[A-Z0-9]$/.test(upper)) return upper;
	const functionKey = /^F([1-9]|1[0-9]|2[0-4])$/i.exec(token);
	if (functionKey) return `F${Number(functionKey[1])}`;
	return namedKeys[token.toLowerCase()] ?? null;
}
function conflictMessage(value, conflicts) {
	const normalized = canonicalizeHotkey(value);
	if (!normalized) return "";
	const match = conflicts.find((candidate) => {
		const other = canonicalizeHotkey(candidate.value);
		return other !== null && other !== "" && other === normalized;
	});
	return match ? t("Already assigned to {label}. Choose another shortcut.", { label: t(match.label) }) : "";
}
function reservedHotkeyMessage(value) {
	const tokens = value.split("+");
	return tokens[tokens.length - 1] === "F12" ? t("F12 is reserved by Windows.") : "";
}
function riskyHotkeyMessage(value) {
	const modifiers = value.split("+").slice(0, -1);
	return modifiers.length === 0 || modifiers.length === 1 && modifiers[0] === "Shift" ? t("System-wide single-key shortcut — use carefully.") : "";
}
function announceChord(value) {
	return value.split("+").join(" plus ");
}
function isPromiseLike(value) {
	return Boolean(value && typeof value === "object" && "then" in value && typeof value.then === "function");
}
function recorderErrorMessage(error, fallback) {
	return error instanceof Error && error.message ? error.message : fallback;
}
function isModifierKey(key) {
	return [
		"Control",
		"Ctrl",
		"Alt",
		"AltGraph",
		"Shift",
		"Meta",
		"OS"
	].includes(key);
}
function modifiersFromEvent(event) {
	const modifiers = [];
	if (event.ctrlKey) modifiers.push("Ctrl");
	if (event.altKey) modifiers.push("Alt");
	if (event.shiftKey) modifiers.push("Shift");
	if (event.metaKey) modifiers.push("Win");
	return modifiers;
}
function keyFromKeyboardEvent(event) {
	const codeLetter = /^Key([A-Z])$/.exec(event.code);
	if (codeLetter) return codeLetter[1];
	const codeDigit = /^Digit([0-9])$/.exec(event.code);
	if (codeDigit) return codeDigit[1];
	const numpadDigit = /^Numpad([0-9])$/.exec(event.code);
	if (numpadDigit) return `Num${numpadDigit[1]}`;
	const codeKeys = {
		Space: "Space",
		NumpadEnter: "Enter",
		NumpadMultiply: "Multiply",
		NumpadAdd: "Add",
		NumpadComma: "Separator",
		NumpadSubtract: "Subtract",
		NumpadDecimal: "Decimal",
		NumpadDivide: "Divide",
		Semicolon: "Semicolon",
		Equal: "Equal",
		Comma: "Comma",
		Minus: "Minus",
		Period: "Period",
		Slash: "Slash",
		Backquote: "Backtick",
		BracketLeft: "LeftBracket",
		Backslash: "Backslash",
		BracketRight: "RightBracket",
		Quote: "Quote"
	};
	if (codeKeys[event.code]) return codeKeys[event.code];
	const eventKeys = {
		" ": "Space",
		Spacebar: "Space",
		ArrowLeft: "Left",
		ArrowUp: "Up",
		ArrowRight: "Right",
		ArrowDown: "Down",
		PrintScreen: "PrintScreen",
		ContextMenu: "Apps",
		Standby: "Sleep",
		BrowserBack: "BrowserBack",
		BrowserForward: "BrowserForward",
		BrowserRefresh: "BrowserRefresh",
		BrowserStop: "BrowserStop",
		BrowserSearch: "BrowserSearch",
		BrowserFavorites: "BrowserFavorites",
		BrowserHome: "BrowserHome",
		AudioVolumeMute: "VolumeMute",
		AudioVolumeDown: "VolumeDown",
		AudioVolumeUp: "VolumeUp",
		MediaTrackNext: "MediaNext",
		MediaTrackPrevious: "MediaPrevious",
		MediaStop: "MediaStop",
		MediaPlayPause: "MediaPlayPause",
		LaunchMail: "LaunchMail",
		LaunchMediaPlayer: "LaunchMediaSelect",
		LaunchApplication1: "LaunchApp1",
		LaunchApplication2: "LaunchApp2"
	};
	if (eventKeys[event.key]) return eventKeys[event.key];
	return canonicalKeyName(event.key);
}
//#endregion
//#region src/views/settings.tsx
var sections = [
	{
		key: "lamp",
		label: "Lamp",
		description: "Address and transport",
		icon: "lamp"
	},
	{
		key: "ui",
		label: "Appearance",
		description: "Native window and material",
		icon: "settings"
	},
	{
		key: "notifications",
		label: "Notifications",
		description: "Toasts, balloons, and sound",
		icon: "bell"
	},
	{
		key: "hotkeys",
		label: "Hotkeys",
		description: "System-wide shortcuts",
		icon: "keyboard"
	},
	{
		key: "automation",
		label: "Automation",
		description: "State-change scripts",
		icon: "refresh"
	},
	{
		key: "integration",
		label: "Integration",
		description: "Startup and local API",
		icon: "network"
	},
	{
		key: "diagnostics",
		label: "Diagnostics",
		description: "Endpoint allowlists",
		icon: "diagnostics"
	}
];
function Settings({ api }) {
	const current = config.value;
	const [disableAPIOpen, setDisableAPIOpen] = d$2(false);
	const [toastOpen, setToastOpen] = d$2(false);
	const [toastMessage, setToastMessage] = d$2(() => t("Rayan Lamp notifications are ready."));
	const [testingToast, setTestingToast] = d$2(false);
	const direct = capabilities.value.transport_direct === true;
	const visualNotifications = capabilities.value.notifications !== false && !direct;
	const audioAvailable = capabilities.value.audio_sfx !== false;
	const visibleSections = sections.filter((item) => {
		switch (item.key) {
			case "ui": return true;
			case "notifications": return visualNotifications || audioAvailable;
			case "hotkeys": return capabilities.value.native_shell !== false && !direct;
			case "automation": return capabilities.value.automation !== false && !direct;
			case "integration": return !direct;
			case "diagnostics": return capabilities.value.diagnostics !== false;
			default: return true;
		}
	});
	if (!current) return /* @__PURE__ */ u("div", {
		class: "view",
		children: /* @__PURE__ */ u("div", {
			class: "loading-card",
			children: t("Loading settings…")
		})
	});
	const section = visibleSections.some((item) => item.key === settingsSection.value) ? settingsSection.value : visibleSections[0]?.key ?? "lamp";
	const patch = (value, immediate = false) => updateConfig(value, immediate);
	return /* @__PURE__ */ u("div", {
		class: "view settings-view",
		children: [
			/* @__PURE__ */ u("header", {
				class: "view-heading",
				children: [/* @__PURE__ */ u("div", { children: [
					/* @__PURE__ */ u("span", {
						class: "eyebrow",
						children: t("Configuration")
					}),
					/* @__PURE__ */ u("h1", { children: t("Settings") }),
					/* @__PURE__ */ u("p", { children: direct ? t("Direct connection preferences are stored in this browser and synchronized across tabs.") : t("Changes apply automatically and reconcile with the native app.") })
				] }), /* @__PURE__ */ u("div", {
					class: `save-indicator${settingsError.value ? " has-error" : ""}`,
					role: "status",
					"aria-live": "polite",
					children: settingsSaving.value ? /* @__PURE__ */ u(S$1, { children: [
						/* @__PURE__ */ u("span", {
							class: "button-spinner",
							"aria-hidden": "true"
						}),
						" ",
						t("Saving…")
					] }) : settingsError.value ? /* @__PURE__ */ u(S$1, { children: [
						/* @__PURE__ */ u(Icon, {
							name: "warning",
							size: 16
						}),
						" ",
						t("Needs attention")
					] }) : /* @__PURE__ */ u(S$1, { children: [
						/* @__PURE__ */ u(Icon, {
							name: "check",
							size: 16
						}),
						" ",
						t("Up to date")
					] })
				})]
			}),
			settingsError.value && /* @__PURE__ */ u("div", {
				class: "error-banner",
				role: "alert",
				children: [
					/* @__PURE__ */ u(Icon, { name: "warning" }),
					/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Latest change was not accepted.") }), /* @__PURE__ */ u("span", { children: t(settingsError.value) })] }),
					/* @__PURE__ */ u(Button, {
						kind: "quiet",
						onClick: () => void flushConfigPatch(),
						children: t("Retry")
					})
				]
			}),
			/* @__PURE__ */ u("div", {
				class: "settings-layout",
				children: [/* @__PURE__ */ u("nav", {
					class: "settings-nav",
					"aria-label": t("Settings categories"),
					children: visibleSections.map((item) => /* @__PURE__ */ u("button", {
						type: "button",
						class: section === item.key ? "is-current" : "",
						"aria-current": section === item.key ? "page" : void 0,
						onClick: () => {
							settingsSection.value = item.key;
						},
						children: [
							/* @__PURE__ */ u(Icon, { name: item.icon }),
							/* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u("strong", { children: t(item.label) }), /* @__PURE__ */ u("small", { children: t(item.description) })] }),
							/* @__PURE__ */ u(Icon, {
								name: "chevron",
								size: 16,
								class: "settings-chevron"
							})
						]
					}, item.key))
				}), /* @__PURE__ */ u(Card, {
					class: "settings-panel",
					children: [
						section === "lamp" && /* @__PURE__ */ u(LampSettings, {
							value: current,
							patch,
							direct
						}),
						section === "ui" && /* @__PURE__ */ u(AppearanceSettings, {
							value: current,
							patch,
							native: capabilities.value.native_shell !== false && !direct
						}),
						section === "notifications" && /* @__PURE__ */ u(NotificationSettings, {
							api,
							value: current,
							patch,
							direct,
							visualAvailable: visualNotifications,
							audioAvailable,
							onTest: () => setToastOpen(true)
						}),
						section === "hotkeys" && /* @__PURE__ */ u(HotkeySettings, {
							api,
							captureAvailable: capabilities.value.hotkey_capture === true,
							probeAvailable: capabilities.value.hotkey_probe === true,
							value: current,
							patch
						}),
						section === "automation" && /* @__PURE__ */ u(AutomationSettings, {
							value: current,
							patch
						}),
						section === "integration" && /* @__PURE__ */ u(IntegrationSettings, {
							api,
							value: current,
							patch,
							onDisableAPI: () => setDisableAPIOpen(true)
						}),
						section === "diagnostics" && /* @__PURE__ */ u(DiagnosticsSettings, {
							value: current,
							patch
						})
					]
				})]
			}),
			/* @__PURE__ */ u(Modal, {
				open: disableAPIOpen,
				title: t("Turn off the local API?"),
				description: t("The bundled web dashboard uses this same private loopback listener. This page will disconnect immediately."),
				onClose: () => setDisableAPIOpen(false),
				children: /* @__PURE__ */ u("div", {
					class: "modal-actions",
					children: [/* @__PURE__ */ u(Button, {
						kind: "quiet",
						onClick: () => setDisableAPIOpen(false),
						children: t("Keep enabled")
					}), /* @__PURE__ */ u(Button, {
						kind: "danger",
						onClick: () => {
							setDisableAPIOpen(false);
							patch({ integration: { local_api_enabled: false } }, true);
						},
						children: t("Disable and disconnect")
					})]
				})
			}),
			/* @__PURE__ */ u(Modal, {
				open: toastOpen,
				title: t("Test a notification"),
				description: t("Visual delivery and sound remain independent and follow the settings above."),
				onClose: () => setToastOpen(false),
				children: [/* @__PURE__ */ u(TextAreaField, {
					label: t("Message"),
					value: toastMessage,
					rows: 3,
					onInput: setToastMessage
				}), /* @__PURE__ */ u("div", {
					class: "modal-actions",
					children: [/* @__PURE__ */ u(Button, {
						kind: "quiet",
						onClick: () => setToastOpen(false),
						children: t("Cancel")
					}), /* @__PURE__ */ u(Button, {
						kind: "primary",
						busy: testingToast,
						disabled: !toastMessage.trim() || testingToast,
						onClick: async () => {
							setTestingToast(true);
							try {
								await api.notify(t("Rayan Lamp test"), toastMessage.trim());
								showToast("success", t("Test notification sent"));
								setToastOpen(false);
							} catch (error) {
								showToast("error", t("Notification test failed"), error instanceof Error ? error.message : void 0);
							} finally {
								setTestingToast(false);
							}
						},
						children: t("Send test")
					})]
				})]
			})
		]
	});
}
function PanelHeading({ title, description }) {
	return /* @__PURE__ */ u("header", {
		class: "panel-heading",
		children: [/* @__PURE__ */ u("h2", { children: t(title) }), /* @__PURE__ */ u("p", { children: t(description) })]
	});
}
function LampSettings({ value, patch, direct }) {
	const lamp = value.lamp;
	return /* @__PURE__ */ u(S$1, { children: [
		/* @__PURE__ */ u(PanelHeading, {
			title: "Lamp connection",
			description: direct ? "This browser talks to the lamp over CORS-enabled HTTP and its firmware WebSocket." : "The native controller owns discovery, HTTP commands, and the resilient event socket."
		}),
		/* @__PURE__ */ u(FieldGroup, {
			title: "Device",
			children: [
				/* @__PURE__ */ u(TextField, {
					label: "Lamp address",
					type: "url",
					value: lamp.base_url,
					placeholder: "http://rayanlamp.local",
					description: "HTTP or HTTPS origin without a path, query, or embedded credentials.",
					onInput: (base_url) => patch({ lamp: { base_url } })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Request timeout",
					dir: "ltr",
					value: lamp.request_timeout,
					description: direct ? "Duration from 100ms through 2m." : "Go duration from 100ms through 5m.",
					onInput: (request_timeout) => patch({ lamp: { request_timeout } })
				}),
				/* @__PURE__ */ u(NumberField, {
					label: "Device terminal port",
					value: lamp.telnet_port,
					min: 1,
					max: 65535,
					description: direct ? "Retained for configuration compatibility; direct browsers cannot open this raw TCP port." : "Audited Telnet-style device console port. The default firmware uses TCP 23.",
					onInput: (telnet_port) => patch({ lamp: { telnet_port } }, true)
				})
			]
		}),
		/* @__PURE__ */ u(FieldGroup, {
			title: "Live updates",
			children: [
				/* @__PURE__ */ u(Toggle, {
					label: "Listen for WebSocket events",
					description: "Connect to /ws, request one state, then passively receive updates.",
					checked: lamp.websocket,
					onChange: (websocket) => patch({ lamp: { websocket } }, true)
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Reconnect minimum",
					dir: "ltr",
					value: lamp.reconnect_min,
					onInput: (reconnect_min) => patch({ lamp: { reconnect_min } })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Reconnect maximum",
					dir: "ltr",
					value: lamp.reconnect_max,
					onInput: (reconnect_max) => patch({ lamp: { reconnect_max } })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "HTTP polling interval",
					dir: "ltr",
					value: lamp.http_poll,
					description: "Use 0s to disable fallback polling.",
					onInput: (http_poll) => patch({ lamp: { http_poll } })
				})
			]
		})
	] });
}
function AppearanceSettings({ value, patch, native = true }) {
	const ui = value.ui;
	const uiPatch = (next, immediate = true) => patch({ ui: next }, immediate);
	return /* @__PURE__ */ u(S$1, { children: [
		/* @__PURE__ */ u(PanelHeading, {
			title: native ? "Native app appearance" : "Web interface appearance",
			description: native ? "Language and theme are shared with this dashboard; the remaining controls affect the Windows window." : "Language, direction, typography, theme, and motion apply to this browser immediately."
		}),
		/* @__PURE__ */ u(FieldGroup, {
			title: native ? "Layout and material" : "Dashboard appearance",
			children: [
				/* @__PURE__ */ u("div", {
					class: "two-column-fields",
					children: [
						native && /* @__PURE__ */ u(SelectField, {
							label: "Window layout",
							value: ui.view_mode,
							options: [{
								value: "compact",
								label: "Compact lamp"
							}, {
								value: "dashboard",
								label: "Larger dashboard"
							}],
							onChange: (view_mode) => uiPatch({ view_mode })
						}),
						/* @__PURE__ */ u(SelectField, {
							label: "Color theme",
							value: ui.theme,
							options: [
								{
									value: "system",
									label: "Use Windows setting"
								},
								{
									value: "light",
									label: "Light"
								},
								{
									value: "dark",
									label: "Dark"
								}
							],
							onChange: (theme) => uiPatch({ theme })
						}),
						/* @__PURE__ */ u(SelectField, {
							label: "Interface language",
							description: "Changes direction, typography, digits, and reader-facing dates immediately.",
							value: ui.locale ?? "auto",
							options: [
								{
									value: "auto",
									label: "Follow system language"
								},
								{
									value: "en",
									label: "English (US)"
								},
								{
									value: "fa",
									label: "فارسی"
								}
							],
							onChange: (locale) => setWebLocale(locale)
						}),
						native && /* @__PURE__ */ u(S$1, { children: [
							/* @__PURE__ */ u(SelectField, {
								label: "Backdrop",
								value: ui.backdrop,
								options: [
									{
										value: "auto",
										label: "Automatic"
									},
									{
										value: "solid",
										label: "Solid (no effect)"
									},
									{
										value: "transparent",
										label: "Transparent"
									},
									{
										value: "blur",
										label: "Blur behind"
									},
									{
										value: "acrylic",
										label: "Acrylic"
									},
									{
										value: "mica",
										label: "Mica"
									},
									{
										value: "tabbed",
										label: "Tabbed"
									}
								],
								onChange: (backdrop) => uiPatch({
									backdrop,
									...backdrop === "transparent" && ui.title_bar_mode === "native" ? {
										title_bar_mode: "custom",
										show_title_bar: true
									} : {}
								})
							}),
							/* @__PURE__ */ u(SelectField, {
								label: "Rendering backend",
								description: "Automatic uses DirectX 11 hardware composition when supported. Choose Compatibility for lower RAM while retaining the same UI features.",
								value: ui.render_backend,
								options: [
									{
										value: "auto",
										label: "Automatic (DirectX when supported)"
									},
									{
										value: "directx",
										label: "DirectX 11 hardware"
									},
									{
										value: "legacy",
										label: "Compatibility renderer (lower RAM)"
									}
								],
								onChange: (render_backend) => uiPatch({ render_backend })
							}),
							/* @__PURE__ */ u(SelectField, {
								label: "Title bar",
								value: ui.title_bar_mode,
								options: [
									{
										value: "none",
										label: "None"
									},
									{
										value: "custom",
										label: "Custom controls"
									},
									{
										value: "native",
										label: "Native Windows title bar"
									}
								],
								onChange: (title_bar_mode) => uiPatch({
									title_bar_mode,
									show_title_bar: title_bar_mode === "custom",
									...title_bar_mode === "native" && ui.backdrop === "transparent" ? { backdrop: "solid" } : {},
									...title_bar_mode === "native" && ui.border_mode === "none" ? { border_mode: "auto" } : {}
								})
							}),
							/* @__PURE__ */ u(SelectField, {
								label: "Window border",
								description: "Automatic keeps edge resizing. None removes the outer frame.",
								value: ui.border_mode,
								options: [{
									value: "auto",
									label: "Automatic (resizable)"
								}, {
									value: "none",
									label: "None (frameless)"
								}],
								onChange: (border_mode) => uiPatch({
									border_mode,
									...border_mode === "none" && ui.title_bar_mode === "native" ? {
										title_bar_mode: "custom",
										show_title_bar: true
									} : {}
								})
							})
						] })
					]
				}),
				native && /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u(Toggle, {
					label: "Rounded corners",
					checked: ui.rounded_corners,
					onChange: (rounded_corners) => uiPatch({ rounded_corners })
				}), /* @__PURE__ */ u(Toggle, {
					label: "Layered Fluent foreground",
					description: "Sharp SVG artwork, alpha-composited controls, reveal lighting, and motion.",
					checked: ui.layered_foreground,
					onChange: (layered_foreground) => uiPatch({ layered_foreground })
				})] }),
				/* @__PURE__ */ u(Toggle, {
					label: "Animations",
					checked: ui.animations,
					onChange: (animations) => uiPatch({ animations })
				})
			]
		}),
		native && /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u(FieldGroup, {
			title: "Window behavior",
			children: [
				/* @__PURE__ */ u(Toggle, {
					label: "Always on top",
					checked: ui.always_on_top,
					onChange: (always_on_top) => uiPatch({ always_on_top })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Close to notification area",
					checked: ui.close_to_tray,
					onChange: (close_to_tray) => uiPatch({ close_to_tray })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Preload and reuse the notification-area menu",
					description: "Prepares the menu invisibly and reuses its window between openings to avoid first-frame flashes. Disable to recreate it each time.",
					checked: ui.prewarm_tray_menu,
					onChange: (prewarm_tray_menu) => uiPatch({ prewarm_tray_menu })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Start minimized",
					checked: ui.start_minimized,
					onChange: (start_minimized) => uiPatch({ start_minimized })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Show taskbar button",
					checked: ui.show_taskbar,
					onChange: (show_taskbar) => uiPatch({ show_taskbar })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Magnetic snap",
					description: "Hold Ctrl while dragging to bypass snapping; release Ctrl to resume it immediately.",
					checked: ui.magnetic_snap,
					onChange: (magnetic_snap) => uiPatch({ magnetic_snap })
				}),
				/* @__PURE__ */ u(NumberField, {
					label: "Snap distance",
					description: "Device-independent pixels; 1 through 128.",
					value: ui.magnetic_snap_distance,
					min: 1,
					max: 128,
					onInput: (magnetic_snap_distance) => uiPatch({ magnetic_snap_distance }, false)
				})
			]
		}), /* @__PURE__ */ u(FieldGroup, {
			title: "Stored window bounds",
			description: "Use -1 for X or Y to request default centering.",
			children: /* @__PURE__ */ u("div", {
				class: "four-column-fields",
				children: [
					"x",
					"y",
					"width",
					"height"
				].map((key) => /* @__PURE__ */ u(NumberField, {
					label: key.toUpperCase(),
					value: ui.window_bounds[key],
					onInput: (next) => uiPatch({ window_bounds: {
						...ui.window_bounds,
						[key]: next
					} }, false)
				}, key))
			})
		})] })
	] });
}
function NotificationSettings({ api, value, patch, direct, visualAvailable, audioAvailable, onTest }) {
	const item = value.notifications;
	const fileInput = A(null);
	const [importing, setImporting] = d$2(false);
	const [testingAudio, setTestingAudio] = d$2(false);
	const update = (next) => patch({ notifications: next }, true);
	const chooseFile = () => fileInput.current?.click();
	const importFile = async (file) => {
		if (!file) return;
		setImporting(true);
		try {
			const result = await api.importAudio(file, revision.value);
			acceptRemoteConfig(result.config, result.revision);
			showToast("success", t("Custom sound imported"), result.config.notifications.sound_name || file.name);
		} catch (error) {
			showToast("error", t("Audio import failed"), error instanceof Error ? error.message : void 0);
		} finally {
			setImporting(false);
			if (fileInput.current) fileInput.current.value = "";
		}
	};
	const testAudio = async () => {
		setTestingAudio(true);
		try {
			await api.playAudio("message", 1);
			showToast("success", direct ? t("Audio played in this browser") : t("Audio test sent to Rayan Lamp"));
		} catch (error) {
			showToast("error", t("Audio test failed"), error instanceof Error ? error.message : void 0);
		} finally {
			setTestingAudio(false);
		}
	};
	return /* @__PURE__ */ u(S$1, { children: [
		/* @__PURE__ */ u(PanelHeading, {
			title: "Notifications and sound",
			description: visualAvailable ? "Visual delivery and audible cues are independent." : "Audible cues remain available without native visual notifications."
		}),
		visualAvailable && /* @__PURE__ */ u(FieldGroup, {
			title: "Visual delivery",
			children: [
				/* @__PURE__ */ u(Toggle, {
					label: "Enable notifications",
					checked: item.enabled,
					onChange: (enabled) => update({ enabled })
				}),
				/* @__PURE__ */ u(SelectField, {
					label: "Visual delivery",
					value: item.delivery,
					options: [
						{
							value: "auto",
							label: "Toast, then balloon fallback"
						},
						{
							value: "toast",
							label: "Modern toast only"
						},
						{
							value: "balloon",
							label: "Legacy tray balloon"
						}
					],
					onChange: (delivery) => update({ delivery })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Confirmed state changes",
					checked: item.state_changes,
					disabled: !item.enabled,
					onChange: (state_changes) => update({ state_changes })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Firmware messages",
					checked: item.custom_message,
					disabled: !item.enabled,
					onChange: (custom_message) => update({ custom_message })
				}),
				/* @__PURE__ */ u("div", {
					class: "inline-action",
					children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Verify delivery") }), /* @__PURE__ */ u("span", { children: t("Send a local test without changing any lamp state.") })] }), /* @__PURE__ */ u(Button, {
						icon: /* @__PURE__ */ u(Icon, { name: "bell" }),
						onClick: onTest,
						children: "Test notification"
					})]
				})
			]
		}),
		(visualAvailable || audioAvailable) && /* @__PURE__ */ u(FieldGroup, {
			title: "Event timing",
			children: /* @__PURE__ */ u(Toggle, {
				label: "Initial synchronization",
				description: "Include the first known lamp state after connecting.",
				checked: item.initial_sync,
				onChange: (initial_sync) => update({ initial_sync })
			})
		}),
		audioAvailable && /* @__PURE__ */ u(FieldGroup, {
			title: "Audio effects",
			description: direct ? "Direct mode plays in the elected browser tab only, so synchronized tabs never echo." : "The coordinator owns playback, including when this dashboard is opened remotely.",
			children: [
				/* @__PURE__ */ u(Toggle, {
					label: "Audible cues",
					description: "State and message sounds work even when visual notifications are disabled.",
					checked: item.sound,
					onChange: (sound) => update({ sound })
				}),
				/* @__PURE__ */ u(SelectField, {
					label: "Sound source",
					value: item.sound_source,
					options: [{
						value: "bundled",
						label: "Bundled Rayan Lamp effects"
					}, {
						value: "custom",
						label: "Custom PCM WAV"
					}],
					onChange: (sound_source) => {
						if (sound_source === "custom" && !item.sound_file) {
							chooseFile();
							return;
						}
						update({ sound_source });
					}
				}),
				/* @__PURE__ */ u("input", {
					ref: fileInput,
					class: "audio-file-input",
					type: "file",
					accept: ".wav,audio/wav,audio/x-wav",
					"aria-label": t("Choose custom WAV"),
					onChange: (event) => void importFile(event.currentTarget.files?.[0])
				}),
				/* @__PURE__ */ u("div", {
					class: "audio-file-card",
					children: [
						/* @__PURE__ */ u("span", {
							class: "audio-file-icon",
							"aria-hidden": "true",
							children: /* @__PURE__ */ u(Icon, { name: "sound" })
						}),
						/* @__PURE__ */ u("span", {
							class: "audio-file-copy",
							children: [/* @__PURE__ */ u("strong", { children: item.sound_file ? item.sound_name || t("Content-addressed custom WAV") : t("No custom sound imported") }), /* @__PURE__ */ u("small", { children: item.sound_file ? `SHA-256 ${shortAssetID(item.sound_file)}` : t("PCM WAV · mono or stereo · up to 4 MiB") })]
						}),
						/* @__PURE__ */ u(Button, {
							kind: "quiet",
							busy: importing,
							disabled: importing,
							icon: /* @__PURE__ */ u(Icon, { name: "upload" }),
							onClick: chooseFile,
							children: item.sound_file ? "Replace" : "Import"
						})
					]
				}),
				/* @__PURE__ */ u("div", {
					class: "inline-action",
					children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Verify audio") }), /* @__PURE__ */ u("span", { children: direct ? t("Play the selected cue in this browser.") : t("Ask the coordinator to play the selected cue once.") })] }), /* @__PURE__ */ u(Button, {
						busy: testingAudio,
						disabled: testingAudio,
						icon: /* @__PURE__ */ u(Icon, { name: "sound" }),
						onClick: () => void testAudio(),
						children: "Test audio"
					})]
				})
			]
		})
	] });
}
function shortAssetID(value) {
	if (value.length <= 18) return value;
	return `${value.slice(0, 10)}…${value.slice(-8)}`;
}
function useHotkeyCaptureSession(api) {
	const [active, setActive] = d$2(null);
	const [errors, setErrors] = d$2({});
	const activeRef = A(null);
	const leaseRef = A(null);
	const acquireRef = A(null);
	const heartbeatRef = A(0);
	const mountedRef = A(true);
	const updateOwner = (key) => {
		activeRef.current = key;
		if (mountedRef.current) setActive(key);
	};
	const updateError = (key, message) => {
		if (!mountedRef.current) return;
		setErrors((current) => {
			if (current[key] === message) return current;
			const next = { ...current };
			if (message) next[key] = message;
			else delete next[key];
			return next;
		});
	};
	const clearHeartbeat = () => {
		window.clearTimeout(heartbeatRef.current);
		heartbeatRef.current = 0;
	};
	const loseCapture = (key, message) => {
		clearHeartbeat();
		leaseRef.current = null;
		if (activeRef.current === key) updateOwner(null);
		updateError(key, message);
	};
	const armHeartbeat = (lease) => {
		clearHeartbeat();
		if (!api || activeRef.current === null) return;
		const ttl = Math.max(1e3, lease.lease_ttl_ms);
		const delay = Math.max(500, Math.floor(ttl * .45));
		heartbeatRef.current = window.setTimeout(async () => {
			if (activeRef.current === null || leaseRef.current?.lease_id !== lease.lease_id) return;
			const owner = activeRef.current;
			try {
				const renewed = await api.renewHotkeyCapture(lease.lease_id);
				if (activeRef.current !== owner || leaseRef.current?.lease_id !== lease.lease_id) return;
				leaseRef.current = renewed;
				armHeartbeat(renewed);
			} catch {
				if (activeRef.current === owner && leaseRef.current?.lease_id === lease.lease_id) loseCapture(owner, "Shortcut capture paused because its lease could not be renewed. Select Record to try again.");
			}
		}, delay);
	};
	const acceptLease = (lease) => {
		leaseRef.current = lease;
		armHeartbeat(lease);
	};
	const ensureLease = async () => {
		if (!api) return null;
		if (leaseRef.current) return leaseRef.current;
		if (acquireRef.current) return acquireRef.current;
		const request = api.beginHotkeyCapture();
		const pending = (async () => {
			const lease = await request;
			if (activeRef.current === null) {
				try {
					await api.releaseHotkeyCapture(lease.lease_id);
				} catch {}
				return null;
			}
			acceptLease(lease);
			return lease;
		})();
		acquireRef.current = pending;
		try {
			return await pending;
		} finally {
			if (acquireRef.current === pending) acquireRef.current = null;
		}
	};
	const begin = (key) => {
		updateOwner(key);
		updateError(key, "");
		if (!api) return { active: true };
		return (async () => {
			try {
				const lease = await ensureLease();
				return { active: activeRef.current === key && lease !== null };
			} catch (error) {
				const message = hotkeyCaptureErrorMessage(error, "Rayan Lamp could not prepare shortcut recording.");
				if (activeRef.current === key) {
					updateOwner(null);
					updateError(key, message);
					return {
						active: false,
						message
					};
				}
				return { active: false };
			}
		})();
	};
	const end = async (key, reason) => {
		if (activeRef.current !== key) return;
		updateOwner(null);
		clearHeartbeat();
		const lease = leaseRef.current;
		leaseRef.current = null;
		if (!api || !lease) return;
		try {
			await api.releaseHotkeyCapture(lease.lease_id);
		} catch (error) {
			if (error instanceof APIError && (error.code === "lease_not_found" || error.status === 404 || error.status === 503)) return;
			const message = reason === "complete" ? "The shortcut was applied, but Windows reported a shortcut restore warning." : "Recording ended, but Windows reported a shortcut restore warning.";
			updateError(key, message);
			return message;
		}
	};
	const probe = async (key, chord) => {
		if (!api) return { available: true };
		if (activeRef.current !== key) throw new Error("This recorder no longer owns the capture session.");
		let lease;
		try {
			lease = await ensureLease();
			if (!lease || activeRef.current !== key) throw new Error("The shortcut capture session ended.");
			const result = await api.probeHotkey(lease.lease_id, chord);
			if (activeRef.current !== key) throw new Error("The shortcut capture session ended.");
			acceptLease(result);
			return {
				available: result.available,
				message: result.message
			};
		} catch (error) {
			const message = hotkeyCaptureErrorMessage(error, "Rayan Lamp could not verify that shortcut. Select Record to try again.");
			if (activeRef.current === key) loseCapture(key, message);
			throw new Error(message);
		}
	};
	h$2(() => {
		mountedRef.current = true;
		const dispose = (keepalive, updateState) => {
			clearHeartbeat();
			const lease = leaseRef.current;
			leaseRef.current = null;
			activeRef.current = null;
			if (updateState && mountedRef.current) setActive(null);
			if (api && lease) api.releaseHotkeyCapture(lease.lease_id, keepalive).catch(() => {});
		};
		const pageHide = () => dispose(true, true);
		window.addEventListener("pagehide", pageHide);
		return () => {
			window.removeEventListener("pagehide", pageHide);
			mountedRef.current = false;
			dispose(false, false);
		};
	}, [api]);
	return {
		active,
		errors,
		begin,
		end,
		probe
	};
}
function HotkeySettings({ api, captureAvailable = false, probeAvailable = false, value, patch }) {
	const item = value.hotkeys;
	const captureAPI = api && captureAvailable ? api : void 0;
	const capture = useHotkeyCaptureSession(captureAPI);
	const bindings = [
		{
			key: "toggle",
			label: "Toggle lamp"
		},
		{
			key: "on",
			label: "Turn on"
		},
		{
			key: "off",
			label: "Turn off"
		},
		{
			key: "show",
			label: "Show window"
		}
	];
	const update = async (key, chord) => {
		patch({ hotkeys: { [key]: chord } }, true);
		if (!api) return;
		await flushConfigPatch();
		if (settingsError.value) throw new Error(settingsError.value);
	};
	return /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u(PanelHeading, {
		title: "Global hotkeys",
		description: "Use modifier+key chords. Leave a field blank to disable it."
	}), /* @__PURE__ */ u(FieldGroup, {
		title: "Bindings",
		description: "Record a chord directly. Completed shortcuts are normalized and synchronized immediately.",
		children: [/* @__PURE__ */ u("div", {
			class: "hotkey-browser-note",
			children: [/* @__PURE__ */ u(Icon, {
				name: "keyboard",
				size: 17
			}), /* @__PURE__ */ u("span", { children: captureAPI ? probeAvailable ? t("Shortcuts are checked with Windows before they are applied. Windows-key and other system-reserved combinations may still be handled before a browser sees them.") : t("This controller can safely pause global shortcuts while you record, but cannot preflight availability. Windows-key and system-reserved combinations are best recorded in native Settings.") : t("Browser-only recording cannot preflight Windows-wide availability. Windows-key and system-reserved combinations are best recorded in native Settings.") })]
		}), /* @__PURE__ */ u("div", {
			class: "hotkey-grid",
			children: bindings.map((binding) => {
				const conflicts = bindings.filter((candidate) => candidate.key !== binding.key).map((candidate) => ({
					label: candidate.label,
					value: item[candidate.key]
				}));
				return /* @__PURE__ */ u(HotkeyRecorder, {
					label: binding.label,
					value: item[binding.key],
					conflicts,
					captureActive: capture.active === binding.key,
					captureError: capture.errors[binding.key],
					onCaptureStart: () => capture.begin(binding.key),
					onCaptureStop: (reason) => capture.end(binding.key, reason),
					onProbe: captureAPI && probeAvailable ? (chord) => capture.probe(binding.key, chord) : void 0,
					onCommit: (chord) => update(binding.key, chord)
				}, binding.key);
			})
		})]
	})] });
}
function hotkeyCaptureErrorMessage(error, fallback) {
	if (error instanceof APIError) {
		if (error.code === "lease_not_found") return "The shortcut capture session expired. Select Record to try again.";
		return error.message || fallback;
	}
	return error instanceof Error && error.message ? error.message : fallback;
}
function AutomationSettings({ value, patch }) {
	const item = value.automation;
	const update = (next, immediate = false) => patch({ automation: next }, immediate);
	return /* @__PURE__ */ u(S$1, { children: [
		/* @__PURE__ */ u(PanelHeading, {
			title: "Event automation",
			description: "Run reviewed local scripts after lamp events and Windows session, power, or network changes."
		}),
		/* @__PURE__ */ u("div", {
			class: "security-note",
			children: [/* @__PURE__ */ u(Icon, { name: "warning" }), /* @__PURE__ */ u("p", { children: t("Scripts run with your Windows account and are not sandboxed. Use trusted absolute paths and never evaluate firmware message text as code.") })]
		}),
		/* @__PURE__ */ u(FieldGroup, {
			title: "Runner",
			children: [
				/* @__PURE__ */ u(Toggle, {
					label: "Enable automation",
					checked: item.enabled,
					onChange: (enabled) => update({ enabled }, true)
				}),
				/* @__PURE__ */ u(TextField, {
					label: "On script",
					dir: "ltr",
					value: item.on_script,
					placeholder: "C:\\\\Automation\\\\lamp-on.ps1",
					onInput: (on_script) => update({ on_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Off script",
					dir: "ltr",
					value: item.off_script,
					placeholder: "C:\\\\Automation\\\\lamp-off.ps1",
					onInput: (off_script) => update({ off_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Message script",
					dir: "ltr",
					value: item.message_script,
					placeholder: "C:\\\\Automation\\\\lamp-message.ps1",
					onInput: (message_script) => update({ message_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Windows lock script",
					dir: "ltr",
					value: item.lock_script,
					placeholder: "C:\\\\Automation\\\\session-lock.ps1",
					onInput: (lock_script) => update({ lock_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Windows unlock script",
					dir: "ltr",
					value: item.unlock_script,
					placeholder: "C:\\\\Automation\\\\session-unlock.ps1",
					onInput: (unlock_script) => update({ unlock_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "System suspend script",
					dir: "ltr",
					value: item.suspend_script,
					placeholder: "C:\\\\Automation\\\\system-suspend.ps1",
					onInput: (suspend_script) => update({ suspend_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "System resume script",
					dir: "ltr",
					value: item.resume_script,
					placeholder: "C:\\\\Automation\\\\system-resume.ps1",
					onInput: (resume_script) => update({ resume_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Network-change script",
					dir: "ltr",
					value: item.network_change_script,
					description: "Runs after Windows reports an IP-interface change.",
					placeholder: "C:\\\\Automation\\\\network-change.ps1",
					onInput: (network_change_script) => update({ network_change_script })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Working directory",
					dir: "ltr",
					value: item.working_directory,
					onInput: (working_directory) => update({ working_directory })
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Execution timeout",
					dir: "ltr",
					value: item.timeout,
					description: "From 1s through 10m.",
					onInput: (timeout) => update({ timeout })
				}),
				/* @__PURE__ */ u(Toggle, {
					label: "Capture script output",
					description: "Bounded stdout/stderr is written to the application log.",
					checked: item.capture_output,
					onChange: (capture_output) => update({ capture_output }, true)
				})
			]
		})
	] });
}
function IntegrationSettings({ api, value, patch, onDisableAPI }) {
	const item = value.integration;
	const [listenDraft, setListenDraft] = d$2(item.local_api_listen);
	const [moveOpen, setMoveOpen] = d$2(false);
	const [moving, setMoving] = d$2(false);
	const [listenError, setListenError] = d$2("");
	const [registration, setRegistration] = d$2(null);
	const [registrationLoading, setRegistrationLoading] = d$2(true);
	const [registrationError, setRegistrationError] = d$2("");
	const [registrationNotice, setRegistrationNotice] = d$2(null);
	const [registrationAction, setRegistrationAction] = d$2(null);
	const [unregisterTarget, setUnregisterTarget] = d$2(null);
	const update = (next, immediate = false) => patch({ integration: next }, immediate);
	h$2(() => {
		if (!moveOpen) setListenDraft(item.local_api_listen);
	}, [item.local_api_listen, moveOpen]);
	const refreshRegistration = async (announceFailure = false) => {
		try {
			const next = await api.integrationRegistrationStatus();
			setRegistration(next);
			setRegistrationError("");
		} catch (error) {
			setRegistration(null);
			const message = errorMessage(error, t("The current executable's registration status could not be read."));
			setRegistrationError(message);
			if (announceFailure) showToast("error", t("Registration status unavailable"), t(message));
		} finally {
			setRegistrationLoading(false);
		}
	};
	h$2(() => {
		let active = true;
		setRegistrationLoading(true);
		setRegistration(null);
		setRegistrationError("");
		api.integrationRegistrationStatus().then((next) => {
			if (!active) return;
			setRegistration(next);
			setRegistrationLoading(false);
		}, (error) => {
			if (!active) return;
			setRegistrationError(errorMessage(error, t("The current executable's registration status could not be read.")));
			setRegistrationLoading(false);
		});
		return () => {
			active = false;
		};
	}, [api]);
	const changed = listenDraft.trim() !== item.local_api_listen;
	const validate = () => validateListenAddress(listenDraft);
	const runRegistrationAction = async (method) => {
		setRegistrationAction(method);
		setRegistrationNotice(null);
		try {
			await api.updateIntegrationRegistration(method);
			showToast("success", registrationSuccessMessage(method));
		} catch (error) {
			const message = errorMessage(error, t("Windows rejected the registration change."));
			const partial = error instanceof APIError && error.code === "partial";
			setRegistrationNotice({
				kind: partial ? "warning" : "error",
				message
			});
			showToast(partial ? "info" : "error", partial ? t("Shell registration partially completed") : t("Registration change failed"), t(message));
		} finally {
			await refreshRegistration();
			setRegistrationAction(null);
		}
	};
	const busy = registrationAction !== null;
	const executable = registration?.executable ?? "";
	const targetCopy = unregisterTarget === "shell" ? {
		name: t("Windows shell integration"),
		method: "integration.shell.unregister",
		description: t("This removes the rayan-lamp URI handler and Jump List tasks owned by this executable.")
	} : {
		name: t("COM automation"),
		method: "integration.com.unregister",
		description: t("This removes the current-user COM registration owned by this executable.")
	};
	return /* @__PURE__ */ u(S$1, { children: [
		/* @__PURE__ */ u(PanelHeading, {
			title: "Windows and local integrations",
			description: "The authenticated API is restricted to the current machine."
		}),
		/* @__PURE__ */ u(FieldGroup, {
			title: "Windows",
			children: /* @__PURE__ */ u(Toggle, {
				label: "Start with Windows",
				checked: item.start_with_windows,
				onChange: (start_with_windows) => update({ start_with_windows }, true)
			})
		}),
		/* @__PURE__ */ u(FieldGroup, {
			title: "Current executable registration",
			description: "Portable mode stays unregistered by default. These one-time actions affect only the executable shown below; they are not saved as settings or replayed at startup.",
			children: [
				/* @__PURE__ */ u("div", {
					class: "registration-executable",
					children: [/* @__PURE__ */ u("span", { children: t("Current executable") }), registrationLoading && !executable ? /* @__PURE__ */ u("span", {
						class: "registration-loading",
						children: [/* @__PURE__ */ u("span", {
							class: "button-spinner",
							"aria-hidden": "true"
						}), t("Reading registration…")]
					}) : executable ? /* @__PURE__ */ u("code", {
						dir: "ltr",
						title: executable,
						children: executable
					}) : /* @__PURE__ */ u("span", { children: t("Not available") })]
				}),
				registrationError && /* @__PURE__ */ u("div", {
					class: "registration-notice is-error",
					role: "alert",
					children: [
						/* @__PURE__ */ u(Icon, { name: "warning" }),
						/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Registration status unavailable") }), /* @__PURE__ */ u("span", { children: t(registrationError) })] }),
						/* @__PURE__ */ u(Button, {
							kind: "quiet",
							disabled: busy,
							onClick: () => void refreshRegistration(true),
							children: "Refresh"
						})
					]
				}),
				registrationNotice && /* @__PURE__ */ u("div", {
					class: `registration-notice is-${registrationNotice.kind}`,
					role: "alert",
					children: [/* @__PURE__ */ u(Icon, { name: "warning" }), /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: registrationNotice.kind === "warning" ? t("Partially completed") : t("Registration was not changed") }), /* @__PURE__ */ u("span", { children: t(registrationNotice.message) })] })]
				}),
				/* @__PURE__ */ u("div", {
					class: "registration-grid",
					children: [/* @__PURE__ */ u("article", {
						class: "registration-card",
						children: [/* @__PURE__ */ u("div", {
							class: "registration-card-heading",
							children: [
								/* @__PURE__ */ u("span", {
									class: "registration-card-icon",
									children: /* @__PURE__ */ u(Icon, { name: "window" })
								}),
								/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Windows shell") }), /* @__PURE__ */ u("span", { children: [
									/* @__PURE__ */ u(BidiLtr, { children: "rayan-lamp://" }),
									" ",
									t("links and useful Jump List commands")
								] })] }),
								/* @__PURE__ */ u(RegistrationState, {
									loading: registrationLoading,
									registered: registration?.shell_registered
								})
							]
						}), /* @__PURE__ */ u("div", {
							class: "registration-card-actions",
							children: registration?.shell_registered ? /* @__PURE__ */ u(Button, {
								kind: "danger",
								disabled: busy,
								onClick: () => setUnregisterTarget("shell"),
								children: "Unregister shell"
							}) : /* @__PURE__ */ u(Button, {
								kind: "secondary",
								busy: registrationAction === "integration.shell.register",
								disabled: busy || registrationLoading || !registration,
								onClick: () => void runRegistrationAction("integration.shell.register"),
								children: "Register shell"
							})
						})]
					}), /* @__PURE__ */ u("article", {
						class: "registration-card",
						children: [/* @__PURE__ */ u("div", {
							class: "registration-card-heading",
							children: [
								/* @__PURE__ */ u("span", {
									class: "registration-card-icon",
									children: /* @__PURE__ */ u(Icon, { name: "settings" })
								}),
								/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("COM automation") }), /* @__PURE__ */ u("span", { children: t("Opt-in control for VBA, AutoHotkey, and other COM clients") })] }),
								/* @__PURE__ */ u(RegistrationState, {
									loading: registrationLoading,
									registered: registration?.com_registered
								})
							]
						}), /* @__PURE__ */ u("div", {
							class: "registration-card-actions",
							children: registration?.com_registered ? /* @__PURE__ */ u(Button, {
								kind: "danger",
								disabled: busy,
								onClick: () => setUnregisterTarget("com"),
								children: "Unregister COM"
							}) : /* @__PURE__ */ u(Button, {
								kind: "secondary",
								busy: registrationAction === "integration.com.register",
								disabled: busy || registrationLoading || !registration,
								onClick: () => void runRegistrationAction("integration.com.register"),
								children: "Register COM"
							})
						})]
					})]
				}),
				/* @__PURE__ */ u("p", {
					class: "field-description",
					children: t("If this portable executable is moved, register its new path again. Unregistration only removes entries still owned by this exact executable.")
				})
			]
		}),
		/* @__PURE__ */ u(FieldGroup, {
			title: "Loopback API",
			children: [
				/* @__PURE__ */ u(Toggle, {
					label: "Enable local API and web dashboard",
					checked: item.local_api_enabled,
					onChange: (local_api_enabled) => {
						if (!local_api_enabled) onDisableAPI();
						else update({ local_api_enabled }, true);
					}
				}),
				/* @__PURE__ */ u(TextField, {
					label: "Listen address",
					dir: "ltr",
					value: listenDraft,
					description: "Changing this address restarts the listener and may move this dashboard.",
					onInput: (next) => {
						setListenDraft(next);
						setListenError("");
					}
				}),
				listenError && /* @__PURE__ */ u("p", {
					class: "validation-message",
					role: "alert",
					children: listenError
				}),
				changed && /* @__PURE__ */ u("div", {
					class: "inline-action",
					children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Dashboard address changed") }), /* @__PURE__ */ u("span", { children: t("Validate, apply, and continue at the new loopback origin.") })] }), /* @__PURE__ */ u(Button, {
						onClick: () => {
							const error = validate();
							if (error) setListenError(error);
							else setMoveOpen(true);
						},
						children: "Apply and move dashboard"
					})]
				}),
				/* @__PURE__ */ u("div", {
					class: "secret-status",
					children: [/* @__PURE__ */ u("span", {
						class: "secret-icon",
						children: /* @__PURE__ */ u(Icon, { name: "check" })
					}), /* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u("strong", { children: t("API secret protected") }), /* @__PURE__ */ u("small", { children: t("The web session never receives or stores the long-lived bearer token.") })] })]
				})
			]
		}),
		/* @__PURE__ */ u(Modal, {
			open: unregisterTarget !== null,
			title: t("Unregister {name}?", { name: targetCopy.name }),
			description: `${targetCopy.description} ${t("Rayan Lamp settings and device data are not removed.")}`,
			onClose: () => !busy && setUnregisterTarget(null),
			children: [executable && /* @__PURE__ */ u("p", {
				class: "registration-confirm-path",
				children: [
					t("Current executable:"),
					" ",
					/* @__PURE__ */ u("code", {
						dir: "ltr",
						children: executable
					})
				]
			}), /* @__PURE__ */ u("div", {
				class: "modal-actions",
				children: [/* @__PURE__ */ u(Button, {
					kind: "quiet",
					disabled: busy,
					onClick: () => setUnregisterTarget(null),
					children: t("Keep registered")
				}), /* @__PURE__ */ u(Button, {
					kind: "danger",
					busy: registrationAction === targetCopy.method,
					disabled: busy,
					onClick: () => {
						const method = targetCopy.method;
						setUnregisterTarget(null);
						runRegistrationAction(method);
					},
					children: t("Unregister")
				})]
			})]
		}),
		/* @__PURE__ */ u(Modal, {
			open: moveOpen,
			title: t("Move the local dashboard?"),
			description: /* @__PURE__ */ u(S$1, { children: [
				t("Rayan Lamp will restart its loopback listener at"),
				" ",
				/* @__PURE__ */ u(BidiLtr, { children: listenDraft.trim() }),
				".",
				" ",
				t("This tab will continue at the new address.")
			] }),
			onClose: () => !moving && setMoveOpen(false),
			children: /* @__PURE__ */ u("div", {
				class: "modal-actions",
				children: [/* @__PURE__ */ u(Button, {
					kind: "quiet",
					disabled: moving,
					onClick: () => setMoveOpen(false),
					children: t("Cancel")
				}), /* @__PURE__ */ u(Button, {
					kind: "primary",
					busy: moving,
					disabled: moving,
					onClick: async () => {
						const nextListen = listenDraft.trim();
						const error = validateListenAddress(nextListen);
						if (error) {
							setListenError(error);
							setMoveOpen(false);
							return;
						}
						setMoving(true);
						try {
							await flushConfigPatch();
							const result = await api.patchConfig({ integration: { local_api_listen: nextListen } }, revision.value);
							acceptRemoteConfig(result.config, result.revision);
						} catch (error) {
							if (!isListenerMoveDisconnect(error)) {
								const message = error instanceof Error ? error.message : t("The listener address was not accepted.");
								setMoving(false);
								setListenError(message);
								showToast("error", t("Listener was not moved"), t(message));
								return;
							}
							showToast("info", t("Listener is moving"));
						}
						const destination = new URL("/app/", `http://${nextListen}`);
						window.location.assign(destination.href);
					},
					children: t("Apply and continue")
				})]
			})
		})
	] });
}
function RegistrationState({ loading, registered }) {
	return /* @__PURE__ */ u("span", {
		class: `registration-state${loading ? " is-loading" : registered ? " is-registered" : " is-unregistered"}`,
		role: "status",
		children: [/* @__PURE__ */ u("span", { "aria-hidden": "true" }), loading ? t("Checking") : registered ? t("Registered") : t("Not registered")]
	});
}
function registrationSuccessMessage(method) {
	switch (method) {
		case "integration.shell.register": return t("Windows shell integration registered");
		case "integration.shell.unregister": return t("Windows shell integration unregistered");
		case "integration.com.register": return t("COM automation registered");
		case "integration.com.unregister": return t("COM automation unregistered");
	}
}
function errorMessage(error, fallback) {
	return error instanceof Error && error.message ? error.message : fallback;
}
function isListenerMoveDisconnect(error) {
	return error instanceof TypeError || error instanceof APIError && error.status === 408;
}
function DiagnosticsSettings({ value, patch }) {
	const item = value.diagnostics;
	return /* @__PURE__ */ u(S$1, { children: [/* @__PURE__ */ u(PanelHeading, {
		title: "Diagnostic endpoint policy",
		description: "Only GET endpoints listed here can be queried through the app."
	}), /* @__PURE__ */ u(FieldGroup, {
		title: "Audited endpoints",
		children: [
			/* @__PURE__ */ u("div", {
				class: "endpoint-policy-list",
				children: auditedEndpoints.map((endpoint) => /* @__PURE__ */ u("code", { children: endpoint }, endpoint))
			}),
			/* @__PURE__ */ u("p", {
				class: "field-description",
				children: t("The firmware audit fixes this allowlist. Unrelated project and internal debug routes are intentionally unavailable.")
			}),
			item.safe_endpoints.some((endpoint) => !new Set(auditedEndpoints).has(endpoint)) && /* @__PURE__ */ u("div", {
				class: "security-note",
				children: [/* @__PURE__ */ u(Icon, { name: "warning" }), /* @__PURE__ */ u("p", { children: t("This configuration contains legacy endpoint entries. The web dashboard never presents or requests them.") })]
			})
		]
	})] });
}
var auditedEndpoints = [
	"/info",
	"/update/info",
	"/wifi/signal",
	"/wifi/status",
	"/wifi/country",
	"/description.xml"
];
function validateListenAddress(value) {
	const trimmed = value.trim();
	if (!trimmed) return t("Enter a loopback host and numeric port.");
	let parsed;
	try {
		parsed = new URL(`http://${trimmed}`);
	} catch {
		return t("Use an address such as 127.0.0.1:47831.");
	}
	if (parsed.username || parsed.password || parsed.pathname !== "/" || parsed.search || parsed.hash || !parsed.port) return t("Use only a loopback host and explicit numeric port.");
	const port = Number(parsed.port);
	if (!Number.isInteger(port) || port < 1 || port > 65535) return t("Port must be between 1 and 65535.");
	const host = parsed.hostname.toLowerCase().replace(/^\[|\]$/g, "");
	if (host !== "localhost" && host !== "::1" && host !== "0:0:0:0:0:0:0:1" && !host.startsWith("127.")) return t("The listener must remain on localhost or a loopback IP.");
	return "";
}
//#endregion
//#region src/terminal-client.ts
var maxTerminalInputFrame = 4096;
var maxTerminalInputBurst = 16384;
var socketOpen = 1;
var socketClosing = 2;
var socketClosed = 3;
/**
* A page-local bridge to the Go coordinator's terminal socket. Terminal
* bytes deliberately never enter BroadcastChannel, localStorage, or the
* application event stream.
*/
var TerminalBridge = class {
	handlers;
	createSocket;
	socket = null;
	disposed = false;
	constructor(handlers, createSocket = (url) => new WebSocket(url)) {
		this.handlers = handlers;
		this.createSocket = createSocket;
	}
	open() {
		if (this.disposed || this.socket && this.socket.readyState !== socketClosed) return;
		this.handlers.onTransportChange?.("opening");
		let socket;
		try {
			socket = this.createSocket(terminalWebSocketURL());
		} catch (error) {
			browserConsole.event("error", "device.event", "Terminal bridge could not be opened", {
				source: "coordinator-terminal",
				action: "open",
				...browserErrorMetadata(error)
			});
			this.handlers.onTransportChange?.("closed");
			this.handlers.onError?.("transport_unavailable", error instanceof Error ? error.message : "The coordinator terminal socket could not be opened.");
			return;
		}
		this.socket = socket;
		socket.binaryType = "arraybuffer";
		socket.addEventListener("open", () => {
			if (this.socket !== socket || this.disposed) return;
			browserConsole.event("success", "device.event", "Terminal bridge ready", {
				source: "coordinator-terminal",
				action: "open"
			});
			this.handlers.onTransportChange?.("ready");
			this.sendControl("status");
		});
		socket.addEventListener("message", (event) => {
			if (this.socket !== socket || this.disposed) return;
			this.acceptMessage(event.data);
		});
		socket.addEventListener("close", (event) => {
			if (this.socket !== socket) return;
			this.socket = null;
			if (!this.disposed) {
				browserConsole.event(event.wasClean ? "info" : "warn", "device.event", "Terminal bridge closed", {
					source: "coordinator-terminal",
					action: "close",
					error_code: event.code ? `close_${event.code}` : "socket_closed"
				});
				this.handlers.onTransportChange?.("closed");
			}
		});
		socket.addEventListener("error", () => {
			if (this.socket !== socket || this.disposed) return;
			browserConsole.event("warn", "device.event", "Terminal bridge was interrupted", {
				source: "coordinator-terminal",
				action: "error",
				error_code: "transport_error"
			});
			this.handlers.onError?.("transport_error", "The coordinator terminal connection was interrupted.");
		});
	}
	connect() {
		return this.sendControl("connect");
	}
	disconnect() {
		return this.sendControl("disconnect");
	}
	requestStatus() {
		return this.sendControl("status");
	}
	sendInput(input) {
		const socket = this.openSocket();
		if (!socket) return false;
		const data = typeof input === "string" ? new TextEncoder().encode(input) : input;
		if (data.byteLength === 0) return true;
		if (data.byteLength > maxTerminalInputBurst) {
			this.handlers.onError?.("input_too_large", "Paste is limited to 16 KiB. Send one short device command at a time.");
			return false;
		}
		for (const frame of terminalInputFrames(input)) {
			const payload = new Uint8Array(frame.byteLength);
			payload.set(frame);
			socket.send(payload.buffer);
		}
		return true;
	}
	dispose() {
		if (this.disposed) return;
		this.disposed = true;
		const socket = this.socket;
		this.socket = null;
		if (socket && socket.readyState !== socketClosing && socket.readyState !== socketClosed) socket.close(1e3, "terminal view closed");
	}
	sendControl(type) {
		const socket = this.openSocket();
		if (!socket) return false;
		socket.send(JSON.stringify({ type }));
		return true;
	}
	openSocket() {
		const socket = this.socket;
		return socket?.readyState === socketOpen ? socket : null;
	}
	async acceptMessage(raw) {
		if (raw instanceof ArrayBuffer) {
			this.handlers.onOutput?.(new Uint8Array(raw));
			return;
		}
		if (ArrayBuffer.isView(raw)) {
			this.handlers.onOutput?.(new Uint8Array(raw.buffer, raw.byteOffset, raw.byteLength));
			return;
		}
		if (raw instanceof Blob) {
			this.handlers.onOutput?.(new Uint8Array(await raw.arrayBuffer()));
			return;
		}
		if (typeof raw !== "string") {
			this.handlers.onError?.("invalid_frame", "The coordinator sent an unsupported terminal frame.");
			return;
		}
		let value;
		try {
			value = JSON.parse(raw);
		} catch {
			this.handlers.onError?.("invalid_frame", "The coordinator sent malformed terminal metadata.");
			return;
		}
		if (!value || typeof value !== "object") {
			this.handlers.onError?.("invalid_frame", "The coordinator sent invalid terminal metadata.");
			return;
		}
		const frame = value;
		switch (frame.type) {
			case "status":
				if (isDeviceTerminalStatus(frame.status)) this.handlers.onStatus?.(frame.status);
				else this.handlers.onError?.("invalid_frame", "The coordinator sent an invalid terminal status.");
				return;
			case "reset":
				this.handlers.onReset?.();
				return;
			case "error":
				this.handlers.onError?.(typeof frame.code === "string" && frame.code ? frame.code : "terminal_error", typeof frame.message === "string" && frame.message ? frame.message : "The device terminal reported an error.");
				return;
			default: this.handlers.onError?.("invalid_frame", "The coordinator sent an unknown terminal control frame.");
		}
	}
};
function terminalWebSocketURL(pageURL = window.location.href) {
	const url = new URL("/ui/terminal", pageURL);
	url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
	return url.href;
}
function terminalInputFrames(input, maximum = maxTerminalInputFrame) {
	if (!Number.isInteger(maximum) || maximum < 4) throw new RangeError("terminal frame limit must be an integer of at least 4");
	const encoded = typeof input === "string" ? new TextEncoder().encode(input) : input;
	const frames = [];
	let offset = 0;
	while (offset < encoded.byteLength) {
		let end = Math.min(offset + maximum, encoded.byteLength);
		if (typeof input === "string" && end < encoded.byteLength) while (end > offset && (encoded[end] & 192) === 128) end--;
		if (end === offset) end = Math.min(offset + maximum, encoded.byteLength);
		frames.push(encoded.slice(offset, end));
		offset = end;
	}
	return frames;
}
function terminalKeyboardInput(input) {
	return input === "\r" ? "\r\n" : input;
}
function isDeviceTerminalStatus(value) {
	if (!value || typeof value !== "object") return false;
	const candidate = value;
	return (candidate.state === "disconnected" || candidate.state === "connecting" || candidate.state === "connected") && typeof candidate.host === "string" && Number.isInteger(candidate.port) && typeof candidate.session === "number" && Number.isInteger(candidate.session) && typeof candidate.sequence === "number" && Number.isInteger(candidate.sequence);
}
//#endregion
//#region src/views/terminal.tsx
var disconnectedStatus = {
	state: "disconnected",
	host: "",
	port: 23,
	session: 0,
	sequence: 0
};
function DeviceTerminal({ api }) {
	const direct = activeClientKind.value === "direct" || capabilities.value.transport_direct === true || api.kind === "direct";
	return !direct && api.kind === "relay" && capabilities.value.device_terminal === true ? /* @__PURE__ */ u(CoordinatorTerminal, { api }) : /* @__PURE__ */ u(TerminalUnavailable, { direct });
}
function CoordinatorTerminal({ api }) {
	const host = A(null);
	const terminal = A(null);
	const bridge = A(null);
	const inputEnabled = A(false);
	const [status, setStatus] = d$2(disconnectedStatus);
	const [transport, setTransport] = d$2("opening");
	const [error, setError] = d$2("");
	h$2(() => {
		let active = true;
		let resizeObserver = null;
		let disposeInput = null;
		let disposeWebGL = null;
		let fit = null;
		Promise.all([__vitePreload(() => import("./xterm-B6pQ9H5k.js"), [], import.meta.url), __vitePreload(() => import("./addon-fit-ZHSQZTEP.js"), [], import.meta.url)]).then(([xtermModule, fitModule]) => {
			if (!active || !host.current) return;
			const xterm = new xtermModule.Terminal({
				allowProposedApi: false,
				allowTransparency: true,
				convertEol: false,
				cursorBlink: config.value?.ui.animations !== false,
				cursorStyle: "bar",
				disableStdin: true,
				drawBoldTextInBrightColors: true,
				fontFamily: "\"Cascadia Mono\", \"Cascadia Code\", Consolas, \"Courier New\", monospace",
				fontSize: 14,
				fontWeight: "400",
				fontWeightBold: 650,
				letterSpacing: 0,
				lineHeight: 1.16,
				logLevel: "info",
				minimumContrastRatio: 4.5,
				rightClickSelectsWord: true,
				screenReaderMode: true,
				scrollback: 5e3,
				scrollOnUserInput: true,
				theme: terminalThemeFor(webTheme.value, resolvedWebTheme.value === "light")
			});
			const fitAddon = new fitModule.FitAddon();
			xterm.loadAddon(fitAddon);
			xterm.open(host.current);
			host.current.dataset.renderer = "canvas";
			__vitePreload(async () => {
				const { WebglAddon } = await import("./addon-webgl-qNQI_Zx4.js");
				return { WebglAddon };
			}, [], import.meta.url).then(({ WebglAddon }) => {
				if (!active || !host.current?.isConnected) return;
				const webgl = new WebglAddon();
				let disposed = false;
				const dispose = () => {
					if (disposed) return;
					disposed = true;
					webgl.dispose();
					if (host.current) host.current.dataset.renderer = "canvas";
				};
				webgl.onContextLoss(dispose);
				try {
					xterm.loadAddon(webgl);
					host.current.dataset.renderer = "webgl";
					disposeWebGL = dispose;
				} catch {
					dispose();
				}
			}).catch(() => {
				if (host.current) host.current.dataset.renderer = "canvas";
			});
			xterm.textarea?.setAttribute("aria-label", t("Rayan Lamp device terminal input"));
			terminal.current = xterm;
			fit = () => {
				if (!active || !host.current?.isConnected) return;
				try {
					fitAddon.fit();
				} catch {}
			};
			window.requestAnimationFrame(fit);
			if (typeof ResizeObserver === "function") {
				resizeObserver = new ResizeObserver(fit);
				resizeObserver.observe(host.current);
			} else window.addEventListener("resize", fit);
			const inputSubscription = xterm.onData((data) => {
				if (inputEnabled.current) bridge.current?.sendInput(terminalKeyboardInput(data));
			});
			disposeInput = () => inputSubscription.dispose();
			const terminalBridge = new TerminalBridge({
				onTransportChange: (phase) => {
					if (!active) return;
					setTransport(phase);
					if (phase === "ready") setError("");
				},
				onStatus: (next) => {
					if (!active) return;
					setStatus(next);
					setError(next.last_error ?? "");
				},
				onOutput: (data) => {
					if (active) xterm.write(data);
				},
				onReset: () => {
					if (!active) return;
					const restoreFocus = document.activeElement === xterm.textarea;
					xterm.blur();
					xterm.reset();
					xterm.clear();
					if (restoreFocus) xterm.focus();
				},
				onError: (_code, message) => {
					if (active) setError(message);
				}
			});
			bridge.current = terminalBridge;
			terminalBridge.open();
		}).catch((loadError) => {
			if (!active) return;
			setTransport("closed");
			setError(loadError instanceof Error ? loadError.message : t("The terminal renderer could not be loaded."));
		});
		return () => {
			active = false;
			resizeObserver?.disconnect();
			if (fit) window.removeEventListener("resize", fit);
			disposeInput?.();
			disposeWebGL?.();
			bridge.current?.dispose();
			bridge.current = null;
			terminal.current?.dispose();
			terminal.current = null;
		};
	}, []);
	const resolvedLight = resolvedWebTheme.value === "light";
	h$2(() => {
		if (terminal.current) terminal.current.options.theme = terminalThemeFor(webTheme.value, resolvedLight);
	}, [resolvedLight, webTheme.value]);
	const canType = terminalAcceptsInput(status, transport);
	const animationsEnabled = config.value?.ui.animations !== false;
	inputEnabled.current = canType;
	h$2(() => {
		if (terminal.current) terminal.current.options.disableStdin = !canType;
	}, [canType]);
	h$2(() => {
		if (terminal.current) terminal.current.options.cursorBlink = animationsEnabled;
	}, [animationsEnabled]);
	const reconnectTransport = () => {
		setError("");
		setTransport("opening");
		api.establishSession(true).then(() => bridge.current?.open()).catch((sessionError) => {
			setTransport("closed");
			setError(sessionError instanceof Error ? sessionError.message : t("The coordinator session could not be renewed."));
		});
	};
	const connectDevice = () => {
		setError("");
		bridge.current?.connect();
	};
	const disconnectDevice = () => {
		setError("");
		bridge.current?.disconnect();
	};
	const clearTerminal = () => {
		const current = terminal.current;
		if (!current) return;
		current.blur();
		current.clear();
		current.focus();
	};
	const shownPort = status.port || config.value?.lamp.telnet_port || disconnectedStatus.port;
	return /* @__PURE__ */ u("div", {
		class: "view terminal-view",
		children: [
			/* @__PURE__ */ u("header", {
				class: "view-heading",
				children: [/* @__PURE__ */ u("div", { children: [
					/* @__PURE__ */ u("span", {
						class: "eyebrow",
						children: t("Coordinator-secured TCP bridge")
					}),
					/* @__PURE__ */ u("h1", { children: t("Device terminal") }),
					/* @__PURE__ */ u("p", { children: t("An ANSI terminal connected to the lamp through the local Go coordinator. Output exists only in this page’s memory.") })
				] }), /* @__PURE__ */ u(TerminalPhase, {
					status: status.state,
					transport
				})]
			}),
			status.read_only && /* @__PURE__ */ u("div", {
				class: "terminal-warning",
				role: "status",
				children: [/* @__PURE__ */ u(Icon, { name: "warning" }), /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Firmware terminal is read-only") }), /* @__PURE__ */ u("span", { children: t("This firmware reports that its interactive shell is assigned to Serial. Network output remains available, but keyboard input is intentionally disabled.") })] })]
			}),
			error && /* @__PURE__ */ u("div", {
				class: "error-banner terminal-error",
				role: "alert",
				children: [/* @__PURE__ */ u(Icon, { name: "warning" }), /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("strong", { children: t("Terminal needs attention") }), /* @__PURE__ */ u("span", { children: t(error) })] })]
			}),
			/* @__PURE__ */ u(Card, {
				class: "terminal-card",
				children: [
					/* @__PURE__ */ u("header", {
						class: "terminal-toolbar",
						children: [/* @__PURE__ */ u("div", {
							class: "terminal-target",
							children: [/* @__PURE__ */ u("span", {
								class: "terminal-target-icon",
								children: /* @__PURE__ */ u(Icon, { name: "terminal" })
							}), /* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u("small", { children: status.host ? /* @__PURE__ */ u(BidiLtr, { children: status.host }) : t("Configured lamp") }), /* @__PURE__ */ u("strong", { children: /* @__PURE__ */ u(BidiLtr, { children: ["TCP ", shownPort] }) })] })]
						}), /* @__PURE__ */ u("div", {
							class: "terminal-actions",
							children: [transport === "closed" ? /* @__PURE__ */ u(Button, {
								kind: "primary",
								icon: /* @__PURE__ */ u(Icon, { name: "reconnect" }),
								onClick: reconnectTransport,
								children: t("Reconnect coordinator")
							}) : status.state === "disconnected" ? /* @__PURE__ */ u(Button, {
								kind: "primary",
								icon: /* @__PURE__ */ u(Icon, { name: "power" }),
								disabled: transport !== "ready",
								busy: transport === "opening",
								onClick: connectDevice,
								children: transport === "opening" ? t("Preparing") : t("Connect")
							}) : /* @__PURE__ */ u(Button, {
								kind: "danger",
								icon: /* @__PURE__ */ u(Icon, { name: "close" }),
								busy: status.state === "connecting",
								disabled: transport !== "ready",
								onClick: disconnectDevice,
								children: t("Disconnect")
							}), /* @__PURE__ */ u(Button, {
								kind: "quiet",
								icon: /* @__PURE__ */ u(Icon, { name: "refresh" }),
								onClick: clearTerminal,
								children: t("Clear")
							})]
						})]
					}),
					/* @__PURE__ */ u("div", {
						class: `terminal-frame${canType ? " accepts-input" : ""}`,
						role: "region",
						"aria-label": t("Rayan Lamp terminal output"),
						onClick: () => terminal.current?.focus(),
						children: [/* @__PURE__ */ u("div", {
							ref: host,
							class: "terminal-host"
						}), status.state !== "connected" && /* @__PURE__ */ u("div", {
							class: "terminal-idle",
							"aria-hidden": "true",
							children: [/* @__PURE__ */ u(Icon, {
								name: "terminal",
								size: 28
							}), /* @__PURE__ */ u("span", { children: status.state === "connecting" ? t("Opening the device terminal…") : t("Connect when you need the device console") })]
						})]
					}),
					/* @__PURE__ */ u("footer", {
						class: "terminal-footer",
						children: [/* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u(Icon, {
							name: canType ? "keyboard" : "info",
							size: 15
						}), canType ? t("Send one short command at a time; firmware lines are limited to 64 ASCII bytes.") : status.read_only ? t("Output-only session") : t("No input is sent while disconnected.")] }), /* @__PURE__ */ u("span", { children: t("Session {session} · Sequence {sequence}", {
							session: status.session ? formatNumber(status.session) : "—",
							sequence: formatNumber(status.sequence)
						}) })]
					})
				]
			}),
			/* @__PURE__ */ u("div", {
				class: "security-note terminal-security-note",
				children: [/* @__PURE__ */ u(Icon, { name: "warning" }), /* @__PURE__ */ u("span", { children: t("Device-shell commands can change firmware settings or restart the lamp. The coordinator fixes the destination to the configured lamp and terminal port; this browser never opens raw TCP itself.") })]
			})
		]
	});
}
function TerminalUnavailable({ direct }) {
	return /* @__PURE__ */ u("div", {
		class: "view terminal-view",
		children: [/* @__PURE__ */ u("header", {
			class: "view-heading",
			children: /* @__PURE__ */ u("div", { children: [
				/* @__PURE__ */ u("span", {
					class: "eyebrow",
					children: t("Device console")
				}),
				/* @__PURE__ */ u("h1", { children: t("Device terminal") }),
				/* @__PURE__ */ u("p", { children: t("The interactive terminal is intentionally isolated behind the Go coordinator.") })
			] })
		}), /* @__PURE__ */ u(Card, {
			class: "terminal-unavailable",
			children: [
				/* @__PURE__ */ u("span", {
					class: "terminal-unavailable-icon",
					children: /* @__PURE__ */ u(Icon, {
						name: "terminal",
						size: 38
					})
				}),
				/* @__PURE__ */ u("span", {
					class: "eyebrow",
					children: t("Coordinator required")
				}),
				/* @__PURE__ */ u("h2", { children: t("Raw TCP is not available in direct browser mode") }),
				/* @__PURE__ */ u("p", { children: t("Browsers cannot connect directly to the lamp’s Telnet port. Choose Automatic or Go coordinator to use the authenticated same-origin terminal bridge.") }),
				direct && /* @__PURE__ */ u("p", {
					class: "terminal-static-note",
					children: [
						t("For the frontend-only ZIP, serve it behind the same origin that forwards"),
						" ",
						/* @__PURE__ */ u("code", {
							dir: "ltr",
							children: "/app/session"
						}),
						" ",
						t("and the versionless API paths to a running RayanLamp coordinator.")
					]
				})
			]
		})]
	});
}
function TerminalPhase({ status, transport }) {
	const label = transport === "opening" ? t("Preparing bridge") : transport === "closed" ? t("Coordinator unavailable") : status === "connected" ? t("Terminal connected") : status === "connecting" ? t("Connecting to device") : t("Terminal disconnected");
	return /* @__PURE__ */ u("span", {
		class: `terminal-phase terminal-phase-${transport === "ready" ? status : transport}`,
		role: "status",
		"aria-live": "polite",
		children: [/* @__PURE__ */ u("span", { "aria-hidden": "true" }), label]
	});
}
function terminalThemeFor(theme, systemPrefersLight) {
	return theme === "light" || theme === "system" && systemPrefersLight ? {
		background: "rgba(245, 250, 253, 0.94)",
		foreground: "#183044",
		cursor: "#006f92",
		cursorAccent: "#f5fafd",
		selectionBackground: "rgba(0, 132, 174, 0.22)",
		selectionForeground: "#102534",
		black: "#183044",
		red: "#b4233c",
		green: "#14754d",
		yellow: "#946400",
		blue: "#006f92",
		magenta: "#7c45a3",
		cyan: "#007f88",
		white: "#dce9ef",
		brightBlack: "#607786",
		brightRed: "#d53950",
		brightGreen: "#16885a",
		brightYellow: "#ad7600",
		brightBlue: "#008bb7",
		brightMagenta: "#9558ba",
		brightCyan: "#0099a2",
		brightWhite: "#ffffff"
	} : {
		background: "rgba(4, 13, 22, 0.9)",
		foreground: "#dcebf3",
		cursor: "#67dcff",
		cursorAccent: "#06111c",
		selectionBackground: "rgba(82, 205, 243, 0.24)",
		selectionForeground: "#f4fbff",
		black: "#07111c",
		red: "#ff6f83",
		green: "#60d89b",
		yellow: "#f1c75b",
		blue: "#67b7ff",
		magenta: "#c99bff",
		cyan: "#67dcff",
		white: "#dcebf3",
		brightBlack: "#728794",
		brightRed: "#ff91a1",
		brightGreen: "#81e7b3",
		brightYellow: "#ffe08a",
		brightBlue: "#8dcbff",
		brightMagenta: "#ddb9ff",
		brightCyan: "#91eaff",
		brightWhite: "#ffffff"
	};
}
function terminalAcceptsInput(status, transport) {
	return transport === "ready" && status.state === "connected" && status.read_only !== true;
}
//#endregion
//#region src/views/diagnostics.tsx
var endpointAllowlist = [
	"/info",
	"/update/info",
	"/wifi/signal",
	"/wifi/status",
	"/wifi/country",
	"/description.xml"
];
function Diagnostics({ api }) {
	const [loading, setLoading] = d$2(!diagnostics.value);
	const [rawEndpoint, setRawEndpoint] = d$2("/info");
	const [raw, setRaw] = d$2("");
	const [rawIsExact, setRawIsExact] = d$2(false);
	const [rawConfirm, setRawConfirm] = d$2(false);
	const [rawLoading, setRawLoading] = d$2(false);
	const current = diagnostics.value;
	const cachedRaw = current?.endpoints[rawEndpoint]?.document.raw ?? "";
	h$2(() => {
		if (diagnostics.value) return;
		let active = true;
		const loadStarted = performance.now();
		api.uiDiagnostics().then((result) => {
			if (active) {
				diagnostics.value = result;
				browserConsole.event("success", "diagnostics.loaded", "Cached diagnostics loaded", {
					source: api.kind,
					count: Object.keys(result.endpoints).length,
					elapsed_ms: Math.round(performance.now() - loadStarted)
				});
			}
		}).catch((error) => {
			if (active) {
				browserConsole.event("error", "diagnostics.error", "Cached diagnostics could not be loaded", {
					source: api.kind,
					elapsed_ms: Math.round(performance.now() - loadStarted),
					...browserErrorMetadata(error)
				});
				showToast("error", t("Diagnostics unavailable"), error instanceof Error ? error.message : void 0);
			}
		}).finally(() => {
			if (active) setLoading(false);
		});
		return () => {
			active = false;
		};
	}, [api]);
	const endpointResults = T(() => endpointAllowlist.map((endpoint) => [endpoint, current?.endpoints[endpoint]]).filter((entry) => Boolean(entry[1])), [current]);
	const refresh = async () => {
		const refreshStarted = performance.now();
		try {
			diagnostics.value = {
				...diagnostics.value ?? emptyDiagnostics(),
				refreshing: true
			};
			const result = await api.refreshUIDiagnostics();
			diagnostics.value = result.diagnostics;
			browserConsole.event(result.warning ? "warn" : "success", "diagnostics.loaded", result.warning ? "Diagnostics refreshed with a warning" : "Diagnostics refreshed", {
				source: api.kind,
				action: result.warning ? "partial" : "complete",
				count: Object.keys(result.diagnostics.endpoints).length,
				elapsed_ms: Math.round(performance.now() - refreshStarted)
			});
			if (result.warning) showToast("info", t("Diagnostics refreshed with a warning"), t(result.warning));
			showToast("success", t("Diagnostics refreshed"));
		} catch (error) {
			if (diagnostics.value) diagnostics.value = {
				...diagnostics.value,
				refreshing: false
			};
			browserConsole.event("error", "diagnostics.error", "Diagnostics refresh failed", {
				source: api.kind,
				action: "refresh",
				elapsed_ms: Math.round(performance.now() - refreshStarted),
				...browserErrorMetadata(error)
			});
			showToast("error", t("Refresh failed"), error instanceof Error ? error.message : void 0);
		}
	};
	return /* @__PURE__ */ u("div", {
		class: "view diagnostics-view",
		children: [/* @__PURE__ */ u("header", {
			class: "view-heading",
			children: [/* @__PURE__ */ u("div", { children: [
				/* @__PURE__ */ u("span", {
					class: "eyebrow",
					children: t("Read-only device data")
				}),
				/* @__PURE__ */ u("h1", { children: t("Diagnostics") }),
				/* @__PURE__ */ u("p", { children: t("Parsed, redacted information from the firmware-audited endpoint set.") })
			] }), /* @__PURE__ */ u(Button, {
				kind: "primary",
				icon: /* @__PURE__ */ u(Icon, { name: "refresh" }),
				busy: current?.refreshing || loading,
				disabled: current?.refreshing || loading,
				onClick: () => void refresh(),
				children: t("Refresh all")
			})]
		}), loading && !current ? /* @__PURE__ */ u(Card, {
			class: "diagnostic-loading",
			children: [/* @__PURE__ */ u("span", {
				class: "button-spinner",
				"aria-hidden": "true"
			}), t("Reading cached device information…")]
		}) : current ? /* @__PURE__ */ u(S$1, { children: [
			/* @__PURE__ */ u("div", {
				class: "diagnostic-summary-grid",
				children: [
					/* @__PURE__ */ u(SummaryCard, {
						title: "Device",
						icon: "info",
						rows: deviceRows(current.device)
					}),
					/* @__PURE__ */ u(SummaryCard, {
						title: "Network",
						icon: "network",
						rows: networkRows(current.device)
					}),
					/* @__PURE__ */ u(SummaryCard, {
						title: "Runtime",
						icon: "diagnostics",
						rows: runtimeRows(current.device)
					}),
					/* @__PURE__ */ u(SummaryCard, {
						title: "Firmware update",
						icon: "refresh",
						rows: updateRows(current.device)
					})
				]
			}),
			/* @__PURE__ */ u(Card, {
				class: "endpoint-card",
				children: [/* @__PURE__ */ u("div", {
					class: "card-heading-row",
					children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("span", {
						class: "eyebrow",
						children: t("Endpoint health")
					}), /* @__PURE__ */ u("h2", { children: t("Audited sources") })] }), /* @__PURE__ */ u("span", {
						class: "updated-label",
						children: t("Updated {date}", { date: formatDate(current.updated_at) })
					})]
				}), /* @__PURE__ */ u("div", {
					class: "endpoint-list",
					children: endpointAllowlist.map((endpoint) => /* @__PURE__ */ u(EndpointStatus, {
						endpoint,
						result: current.endpoints[endpoint]
					}, endpoint))
				})]
			}),
			endpointResults.map(([endpoint, result]) => result.document.fields.length > 0 ? /* @__PURE__ */ u(Card, {
				class: "field-card",
				children: [
					/* @__PURE__ */ u("div", {
						class: "card-heading-row",
						children: [/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("span", {
							class: "eyebrow",
							children: /* @__PURE__ */ u(BidiLtr, { children: result.document.format })
						}), /* @__PURE__ */ u("h2", { children: /* @__PURE__ */ u(BidiLtr, { children: endpoint }) })] }), /* @__PURE__ */ u("span", {
							class: "updated-label",
							children: formatBytes(result.document.bytes)
						})]
					}),
					/* @__PURE__ */ u("div", {
						class: "diagnostic-fields",
						children: result.document.fields.map((field) => /* @__PURE__ */ u(DiagnosticValue, { field }, field.key))
					}),
					result.document.warnings?.map((warning) => /* @__PURE__ */ u("p", {
						class: "diagnostic-warning",
						children: [/* @__PURE__ */ u(Icon, {
							name: "warning",
							size: 16
						}), t(warning)]
					}, warning))
				]
			}, endpoint) : null),
			/* @__PURE__ */ u(Card, {
				class: "advanced-card",
				children: /* @__PURE__ */ u("details", { children: [/* @__PURE__ */ u("summary", { children: [/* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u(Icon, { name: "terminal" }), t("Advanced raw response")] }), /* @__PURE__ */ u("small", { children: t("Cached redacted output by default") })] }), /* @__PURE__ */ u("div", {
					class: "advanced-content",
					children: [
						/* @__PURE__ */ u("p", { children: t("The cached response below is the backend's redacted SafeRaw projection. An explicit exact fetch is available with a sensitivity warning and is always rendered as plain text.") }),
						/* @__PURE__ */ u("div", {
							class: "advanced-toolbar",
							children: [/* @__PURE__ */ u(SelectField, {
								label: "Endpoint",
								value: rawEndpoint,
								options: endpointAllowlist.map((endpoint) => ({
									value: endpoint,
									label: endpoint
								})),
								onChange: (endpoint) => {
									setRawEndpoint(endpoint);
									setRaw("");
									setRawIsExact(false);
								}
							}), /* @__PURE__ */ u(Button, {
								busy: rawLoading,
								disabled: rawLoading,
								kind: "danger",
								onClick: () => setRawConfirm(true),
								children: t("Fetch exact raw")
							})]
						}),
						rawIsExact && /* @__PURE__ */ u("div", {
							class: "security-note",
							role: "status",
							children: [/* @__PURE__ */ u(Icon, { name: "warning" }), /* @__PURE__ */ u("p", { children: t("Exact device response: review before copying or sharing. It may contain sensitive identifiers or network data.") })]
						}),
						/* @__PURE__ */ u("pre", {
							class: "raw-output",
							dir: "ltr",
							tabIndex: 0,
							children: raw || cachedRaw || t("No cached redacted response is available for this endpoint.")
						})
					]
				})] })
			}),
			/* @__PURE__ */ u(Modal, {
				open: rawConfirm,
				title: t("Fetch the exact device response?"),
				description: t("This bypasses the cached redacted view. The response may contain sensitive device identifiers or local network information."),
				onClose: () => setRawConfirm(false),
				children: /* @__PURE__ */ u("div", {
					class: "modal-actions",
					children: [/* @__PURE__ */ u(Button, {
						kind: "quiet",
						onClick: () => setRawConfirm(false),
						children: t("Keep redacted view")
					}), /* @__PURE__ */ u(Button, {
						kind: "danger",
						onClick: async () => {
							setRawConfirm(false);
							setRawLoading(true);
							const exactStarted = performance.now();
							try {
								const result = await api.diagnostics(rawEndpoint);
								setRaw(result.content);
								setRawIsExact(true);
								browserConsole.event("success", "diagnostics.loaded", "Exact diagnostic response fetched", {
									source: api.kind,
									endpoint: rawEndpoint,
									bytes: result.bytes,
									elapsed_ms: Math.round(performance.now() - exactStarted)
								});
							} catch (error) {
								browserConsole.event("error", "diagnostics.error", "Exact diagnostic response failed", {
									source: api.kind,
									endpoint: rawEndpoint,
									elapsed_ms: Math.round(performance.now() - exactStarted),
									...browserErrorMetadata(error)
								});
								setRaw(error instanceof Error ? error.message : t("Unable to fetch endpoint."));
								setRawIsExact(false);
							} finally {
								setRawLoading(false);
							}
						},
						children: t("Fetch exact response")
					})]
				})
			})
		] }) : /* @__PURE__ */ u(Card, {
			class: "empty-card",
			children: [
				/* @__PURE__ */ u(Icon, {
					name: "warning",
					size: 32
				}),
				/* @__PURE__ */ u("h2", { children: t("No diagnostic snapshot") }),
				/* @__PURE__ */ u("p", { children: t("The controller has not returned parsed device information yet.") }),
				/* @__PURE__ */ u(Button, {
					onClick: () => void refresh(),
					children: t("Try again")
				})
			]
		})]
	});
}
function SummaryCard({ title, icon, rows }) {
	return /* @__PURE__ */ u(Card, {
		class: "summary-card",
		children: [/* @__PURE__ */ u("div", {
			class: "summary-title",
			children: [/* @__PURE__ */ u("span", { children: /* @__PURE__ */ u(Icon, { name: icon }) }), /* @__PURE__ */ u("h2", { children: t(title) })]
		}), /* @__PURE__ */ u("dl", { children: rows.map(([label, value]) => /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t(label) }), /* @__PURE__ */ u("dd", { children: value ? isTechnicalSummary(label) ? /* @__PURE__ */ u(BidiLtr, { children: value }) : t(value) : t("Not reported") })] }, label)) })]
	});
}
function EndpointStatus({ endpoint, result }) {
	return /* @__PURE__ */ u("div", {
		class: "endpoint-row",
		children: [
			/* @__PURE__ */ u("span", {
				class: `endpoint-state endpoint-${result?.error ? "error" : result?.stale ? "stale" : result ? "ok" : "idle"}`,
				"aria-hidden": "true"
			}),
			/* @__PURE__ */ u("code", {
				dir: "ltr",
				children: endpoint
			}),
			/* @__PURE__ */ u("span", { children: result?.error ? t(result.error) : result?.stale ? t("Cached result") : result ? t("Fetched {date}", { date: formatDate(result.last_success) }) : t("Not fetched") })
		]
	});
}
function DiagnosticValue({ field }) {
	return /* @__PURE__ */ u("div", {
		class: field.redacted ? "is-redacted" : "",
		children: [/* @__PURE__ */ u("span", { children: t(field.label) }), /* @__PURE__ */ u("strong", { children: field.redacted ? t("Protected") : diagnosticFieldIsTechnical(field) ? /* @__PURE__ */ u(BidiLtr, { children: field.value || "—" }) : formatDiagnosticField(field) })]
	});
}
function deviceRows(device) {
	return [
		["Name", device.device_name || device.description?.friendly_name || ""],
		["Hostname", device.hostname || ""],
		["Model", device.description?.model_name || ""],
		["Firmware", device.firmware || device.build || ""],
		["State", device.reported_state || ""]
	];
}
function networkRows(device) {
	return [
		["Wi-Fi", device.wifi_connected === void 0 ? "" : device.wifi_connected ? t("Connected") : t("Disconnected")],
		["SSID", device.ssid || ""],
		["Address", device.wifi_ip || ""],
		["Station status", device.station_status || ""],
		["Access point", device.access_point_ssid || device.access_point_status || ""],
		["AP address", device.access_point_ip || ""],
		["Signal", device.signal_quality || (device.rssi_dbm === void 0 ? "" : `${formatNumber(device.rssi_dbm)} dBm`)],
		["Country", device.country || ""]
	];
}
function runtimeRows(device) {
	return [
		["Uptime", device.uptime_seconds === void 0 ? "" : formatDuration(device.uptime_seconds)],
		["Free heap", device.heap_free_bytes === void 0 ? "" : formatBytes(device.heap_free_bytes)],
		["Program usage", device.program_usage_percent === void 0 ? "" : `${formatNumber(device.program_usage_percent, { maximumFractionDigits: 1 })}%`],
		["Protocol", device.protocol_revision || ""],
		["Firmware integrity", device.firmware_corrupted === void 0 ? "" : device.firmware_corrupted ? t("Attention required") : t("Verified")]
	];
}
function updateRows(device) {
	const update = device.update ?? {};
	return [
		["Build", update.build || device.build || ""],
		["Hash", update.hash || device.firmware_hash || ""],
		["Image size", update.size_bytes === void 0 ? "" : formatBytes(update.size_bytes)],
		["Free space", update.free_bytes === void 0 ? "" : formatBytes(update.free_bytes)],
		["Date", formatDate(update.date)]
	];
}
function formatDate(value) {
	if (!value) return t("Not reported");
	const parsed = new Date(value);
	if (Number.isNaN(parsed.valueOf())) return value;
	return formatDateTime(parsed, {
		dateStyle: "medium",
		timeStyle: "short"
	});
}
function formatBytes(value) {
	if (!Number.isFinite(value)) return t("Not reported");
	const units = [
		"B",
		"KB",
		"MB",
		"GB"
	];
	let amount = value;
	let index = 0;
	while (Math.abs(amount) >= 1024 && index < units.length - 1) {
		amount /= 1024;
		index++;
	}
	return `${formatNumber(amount, {
		minimumFractionDigits: 0,
		maximumFractionDigits: index === 0 ? 0 : 1
	})} ${units[index]}`;
}
function formatDuration(seconds) {
	const days = Math.floor(seconds / 86400);
	const hours = Math.floor(seconds % 86400 / 3600);
	const minutes = Math.floor(seconds % 3600 / 60);
	return [
		days ? t("{count}d", { count: formatNumber(days) }) : "",
		hours ? t("{count}h", { count: formatNumber(hours) }) : "",
		t("{count}m", { count: formatNumber(minutes) })
	].filter(Boolean).join(" ");
}
function isTechnicalSummary(label) {
	return [
		"Hostname",
		"Firmware",
		"SSID",
		"Address",
		"Station status",
		"Access point",
		"AP address",
		"Protocol",
		"Build",
		"Hash"
	].includes(label);
}
function diagnosticFieldIsTechnical(field) {
	return [
		"ip",
		"mac",
		"hash",
		"time"
	].includes(field.kind);
}
function formatDiagnosticField(field) {
	switch (field.kind) {
		case "integer": return field.integer === void 0 ? localizeDiagnosticDigits(field.value) : formatNumber(field.integer);
		case "number": return field.number === void 0 ? localizeDiagnosticDigits(field.value) : formatNumber(field.number, { maximumFractionDigits: 2 });
		case "bytes": return field.integer === void 0 ? localizeDiagnosticDigits(field.value) : formatBytes(field.integer);
		case "percent": return field.number === void 0 ? localizeDiagnosticDigits(field.value) : `${formatNumber(field.number, { maximumFractionDigits: 1 })}%`;
		case "signal": return field.number === void 0 ? localizeDiagnosticDigits(field.value) : `${formatNumber(field.number, { maximumFractionDigits: 1 })} dBm`;
		case "duration": return field.duration_seconds === void 0 ? localizeDiagnosticDigits(field.value) : formatDuration(field.duration_seconds);
		case "boolean": return field.boolean === void 0 ? t(field.value || "—") : field.boolean ? t("Yes") : t("No");
		default: return t(field.value || "—");
	}
}
function localizeDiagnosticDigits(value) {
	return value.replace(/\d+(?:\.\d+)?/g, (part) => {
		const number = Number(part);
		return Number.isFinite(number) ? formatNumber(number, { maximumFractionDigits: 2 }) : part;
	});
}
function emptyDiagnostics() {
	return {
		device_url: "",
		device: {},
		endpoints: {},
		refreshing: false
	};
}
//#endregion
//#region src/views/about.tsx
function About() {
	const info = version.value;
	const realtimeDisabled = capabilities.value.websocket_events === false;
	return /* @__PURE__ */ u("div", {
		class: "view about-view",
		children: [/* @__PURE__ */ u("header", {
			class: "view-heading",
			children: /* @__PURE__ */ u("div", { children: [
				/* @__PURE__ */ u("span", {
					class: "eyebrow",
					children: t("Application")
				}),
				/* @__PURE__ */ u("h1", { children: t("About Rayan Lamp") }),
				/* @__PURE__ */ u("p", { children: t("A private, single-executable Windows controller by YektaYar.") })
			] })
		}), /* @__PURE__ */ u("div", {
			class: "about-grid",
			children: [
				/* @__PURE__ */ u(Card, {
					class: "about-identity",
					children: [
						/* @__PURE__ */ u("div", {
							class: "about-mark",
							"aria-hidden": "true",
							children: /* @__PURE__ */ u(Icon, {
								name: "lamp",
								size: 42
							})
						}),
						/* @__PURE__ */ u("div", { children: [
							/* @__PURE__ */ u("span", {
								class: "eyebrow",
								children: info?.company || "YektaYar"
							}),
							/* @__PURE__ */ u("h2", { children: info?.display_name || t("Rayan Lamp") }),
							/* @__PURE__ */ u("p", { children: t(info?.description || "Local controller") })
						] }),
						/* @__PURE__ */ u("dl", { children: [
							/* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Version") }), /* @__PURE__ */ u("dd", { children: /* @__PURE__ */ u(BidiLtr, { children: info?.version || "1.0.0" }) })] }),
							info?.edition && /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Edition") }), /* @__PURE__ */ u("dd", { children: t(info.edition === "production" ? "Production" : "Development") })] }),
							info?.commit && /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Commit") }), /* @__PURE__ */ u("dd", { children: /* @__PURE__ */ u("code", {
								dir: "ltr",
								children: info.commit
							}) })] }),
							info?.date && /* @__PURE__ */ u("div", { children: [/* @__PURE__ */ u("dt", { children: t("Built") }), /* @__PURE__ */ u("dd", { children: /* @__PURE__ */ u(BidiLtr, { children: info.date }) })] })
						] })
					]
				}),
				/* @__PURE__ */ u(Card, {
					class: "privacy-card",
					children: [
						/* @__PURE__ */ u("span", {
							class: "card-icon",
							children: /* @__PURE__ */ u(Icon, { name: "network" })
						}),
						/* @__PURE__ */ u("h2", { children: t("Local by design") }),
						/* @__PURE__ */ u("p", { children: activeClientKind.value === "direct" ? t("Direct mode sends simple requests from this browser to the configured lamp. No cloud service or manager token is involved.") : t("The Go coordinator serves this dashboard on a loopback-only listener. Its temporary browser session never exposes the long-lived API bearer token.") })
					]
				}),
				/* @__PURE__ */ u(Card, {
					class: "privacy-card",
					children: [
						/* @__PURE__ */ u("span", {
							class: "card-icon",
							children: /* @__PURE__ */ u(Icon, { name: "check" })
						}),
						/* @__PURE__ */ u("h2", { children: t("Shared state") }),
						/* @__PURE__ */ u("p", { children: activeClientKind.value === "direct" ? t("Tabs share one elected firmware event socket and fan confirmed lamp state through BroadcastChannel.") : capabilities.value.native_shell === false ? t("The headless web host, CLI, terminal UI, IPC clients, and automation share one command dispatcher and confirmed lamp snapshot.") : t("Native UI, tray, CLI, terminal UI, automation, and this web client all reach the same command dispatcher and confirmed lamp snapshot.") })
					]
				}),
				/* @__PURE__ */ u(Card, {
					class: "capability-card",
					children: [
						/* @__PURE__ */ u("div", {
							class: "card-heading-row",
							children: [/* @__PURE__ */ u("h2", { children: t("Web session") }), /* @__PURE__ */ u("span", {
								class: `mini-status ${eventConnected.value ? "is-active" : "is-inactive"}`,
								children: [/* @__PURE__ */ u("span", { "aria-hidden": "true" }), realtimeDisabled ? t("HTTP polling") : eventConnected.value ? t("Events connected") : t("Events reconnecting")]
							})]
						}),
						/* @__PURE__ */ u("p", {
							class: "capability-transport",
							children: [
								t("Active transport:"),
								" ",
								/* @__PURE__ */ u("strong", { children: t(activeClientLabel.value) })
							]
						}),
						/* @__PURE__ */ u("ul", { children: Object.entries(capabilities.value).map(([name, enabled]) => /* @__PURE__ */ u("li", { children: [/* @__PURE__ */ u(Icon, {
							name: enabled ? "check" : "close",
							size: 16
						}), /* @__PURE__ */ u("span", { children: name.replaceAll("_", " ") })] }, name)) })
					]
				})
			]
		})]
	});
}
//#endregion
//#region src/app.tsx
var navigation = [
	{
		route: "dashboard",
		label: "Dashboard",
		icon: "lamp"
	},
	{
		route: "settings",
		label: "Settings",
		icon: "settings"
	},
	{
		route: "terminal",
		label: "Terminal",
		icon: "terminal"
	},
	{
		route: "diagnostics",
		label: "Diagnostics",
		icon: "diagnostics"
	},
	{
		route: "about",
		label: "About",
		icon: "about"
	}
];
function App({ api: suppliedAPI } = {}) {
	const [preferences, setPreferences] = d$2(readConnectionPreferences);
	const api = T(() => suppliedAPI ?? createRayanLampClient(preferences), [suppliedAPI, preferences]);
	const [fatalError, setFatalError] = d$2("");
	const [retry, setRetry] = d$2(0);
	const [connectionOpen, setConnectionOpen] = d$2(false);
	const [updateReady, setUpdateReady] = d$2(false);
	const [applyUpdate, setApplyUpdate] = d$2(null);
	const syncRef = A(null);
	const acceptControllerSnapshot = q$1((next) => {
		const sync = syncRef.current;
		if (sync) {
			sync.acceptSnapshot(next);
			return;
		}
		applySnapshot(next);
	}, []);
	h$2(() => {
		const stopThemeController = startWebThemeController();
		const stopLocaleController = startWebLocaleController();
		const stopFaviconController = startWebFaviconController();
		const stopAudioUnlock = browserAudio.armUserGestureUnlock();
		route.value = routeFromHash(window.location.hash);
		const onHashChange = () => {
			route.value = routeFromHash(window.location.hash);
		};
		window.addEventListener("hashchange", onHashChange);
		return () => {
			stopThemeController();
			stopLocaleController();
			stopFaviconController();
			stopAudioUnlock();
			window.removeEventListener("hashchange", onHashChange);
		};
	}, []);
	h$2(() => {
		if (suppliedAPI) return;
		return subscribeConnectionPreferences(setPreferences);
	}, [suppliedAPI]);
	h$2(() => {
		let active = true;
		let sync = null;
		let poll = 0;
		configureSettingsPersistence(api);
		activeClientKind.value = api.kind;
		activeClientLabel.value = api.displayName;
		beginControllerConnection();
		setFatalError("");
		const transportStarted = performance.now();
		(async () => {
			try {
				const bootstrap = await api.bootstrap();
				if (!active) return;
				activeClientKind.value = api.kind;
				activeClientLabel.value = api.kind === "relay" && bootstrap.capabilities?.native_shell === false ? "Headless Go coordinator" : api.displayName;
				applyBootstrap(bootstrap);
				browserConsole.event("success", "transport.ready", `${activeClientLabel.value} is ready`, {
					source: api.kind,
					action: "bootstrap",
					elapsed_ms: Math.round(performance.now() - transportStarted)
				});
				sync = new RealtimeSync(api, { connectEvents: bootstrap.capabilities?.websocket_events !== false });
				syncRef.current = sync;
				sync.start();
				const configuredPoll = parseDurationMilliseconds(bootstrap.config.lamp.http_poll);
				if (configuredPoll !== 0) poll = window.setInterval(async () => {
					if (!active || eventConnected.value) return;
					try {
						sync?.acceptSnapshot(await api.status());
					} catch {}
				}, Math.max(1e3, configuredPoll ?? 5e3));
			} catch (error) {
				if (!active) return;
				appPhase.value = "offline";
				browserConsole.event("error", "transport.error", "The selected controller transport could not start", {
					source: api.kind,
					action: "bootstrap",
					elapsed_ms: Math.round(performance.now() - transportStarted),
					...browserErrorMetadata(error)
				});
				setFatalError(error instanceof Error ? error.message : "The local Rayan Lamp controller is unavailable.");
			}
		})();
		return () => {
			active = false;
			window.clearInterval(poll);
			sync?.stop();
			if (syncRef.current === sync) syncRef.current = null;
			configureSettingsPersistence(null);
		};
	}, [api, retry]);
	h$2(() => {
		if (!window.isSecureContext || !("serviceWorker" in navigator)) return;
		const update = registerSW({
			immediate: true,
			onNeedRefresh() {
				setApplyUpdate(() => update);
				setUpdateReady(true);
			},
			onOfflineReady() {
				showToast("success", t("Offline fallback ready"), t("The app shell is cached; controls reconnect through the selected local transport."));
			}
		});
	}, []);
	const view = T(() => {
		switch (route.value) {
			case "settings": return /* @__PURE__ */ u(Settings, { api });
			case "terminal": return /* @__PURE__ */ u(DeviceTerminal, { api });
			case "diagnostics": return /* @__PURE__ */ u(Diagnostics, { api });
			case "about": return /* @__PURE__ */ u(About, {});
			default: return /* @__PURE__ */ u(Dashboard, {
				api,
				onSnapshot: acceptControllerSnapshot
			});
		}
	}, [
		acceptControllerSnapshot,
		api,
		route.value
	]);
	const realtimeDisabled = capabilities.value.websocket_events === false;
	const connectionIndicatorActive = eventConnected.value || realtimeDisabled && (appPhase.value === "online" || appPhase.value === "degraded");
	return /* @__PURE__ */ u("div", {
		class: "app-shell",
		children: [
			/* @__PURE__ */ u("a", {
				class: "skip-link",
				href: "#main-content",
				children: t("Skip to content")
			}),
			/* @__PURE__ */ u("aside", {
				class: "app-rail",
				children: [
					/* @__PURE__ */ u("button", {
						type: "button",
						class: "brand",
						"aria-label": t("Open dashboard"),
						onClick: () => navigate("dashboard"),
						children: [/* @__PURE__ */ u("span", {
							class: "brand-mark",
							children: /* @__PURE__ */ u(Icon, {
								name: "lamp",
								size: 28
							})
						}), /* @__PURE__ */ u("span", {
							class: "brand-copy",
							children: [/* @__PURE__ */ u("strong", { children: t("Rayan Lamp") }), /* @__PURE__ */ u("small", { children: t("Local controller") })]
						})]
					}),
					/* @__PURE__ */ u("nav", {
						"aria-label": t("Primary navigation"),
						children: navigation.map((item) => /* @__PURE__ */ k$1(NavButton, {
							...item,
							key: item.route
						}))
					}),
					/* @__PURE__ */ u("div", {
						class: "rail-footer",
						children: [/* @__PURE__ */ u("button", {
							type: "button",
							class: "rail-transport",
							title: t("Connection: {name}", { name: t(activeClientLabel.value) }),
							onClick: () => setConnectionOpen(true),
							children: [/* @__PURE__ */ u(Icon, {
								name: activeClientKind.value === "direct" ? "lamp" : "network",
								size: 15
							}), /* @__PURE__ */ u("span", { children: t(activeClientLabel.value) })]
						}), /* @__PURE__ */ u("span", {
							class: `rail-connection ${connectionIndicatorActive ? "is-connected" : ""}`,
							title: realtimeDisabled ? t("WebSocket events disabled; HTTP polling is active") : eventConnected.value ? t("Realtime events connected") : t("Realtime events reconnecting"),
							children: [/* @__PURE__ */ u("span", { "aria-hidden": "true" }), /* @__PURE__ */ u("span", { children: realtimeDisabled ? t("HTTP polling") : eventConnected.value ? t("Events connected") : t("Reconnecting") })]
						})]
					})
				]
			}),
			/* @__PURE__ */ u("div", {
				class: "app-stage",
				children: [/* @__PURE__ */ u("header", {
					class: "top-bar",
					children: [/* @__PURE__ */ u("div", {
						class: "mobile-brand",
						children: [/* @__PURE__ */ u("span", {
							class: "brand-mark",
							children: /* @__PURE__ */ u(Icon, {
								name: "lamp",
								size: 22
							})
						}), /* @__PURE__ */ u("strong", { children: t("Rayan Lamp") })]
					}), /* @__PURE__ */ u("div", {
						class: "top-bar-actions",
						children: [/* @__PURE__ */ u("button", {
							type: "button",
							class: "transport-button",
							onClick: () => setConnectionOpen(true),
							title: t("Choose how this dashboard connects"),
							children: [
								/* @__PURE__ */ u("span", {
									class: `transport-glyph transport-${activeClientKind.value}`,
									children: /* @__PURE__ */ u(Icon, {
										name: activeClientKind.value === "direct" ? "lamp" : "network",
										size: 17
									})
								}),
								/* @__PURE__ */ u("span", { children: [/* @__PURE__ */ u("small", { children: t("Connection") }), /* @__PURE__ */ u("strong", { children: t(activeClientLabel.value) })] }),
								/* @__PURE__ */ u(Icon, {
									name: "chevron",
									size: 14
								})
							]
						}), /* @__PURE__ */ u("div", {
							class: "theme-switcher",
							role: "group",
							"aria-label": t("Color theme"),
							children: [
								"system",
								"light",
								"dark"
							].map((theme) => /* @__PURE__ */ u("button", {
								type: "button",
								class: webTheme.value === theme ? "is-selected" : "",
								"aria-pressed": webTheme.value === theme,
								title: t("{theme} theme", { theme: themeLabel(theme) }),
								onClick: () => setWebTheme(theme),
								children: [/* @__PURE__ */ u(Icon, {
									name: theme === "system" ? "system" : theme === "light" ? "sun" : "moon",
									size: 17
								}), /* @__PURE__ */ u("span", { children: themeLabel(theme) })]
							}, theme))
						})]
					})]
				}), /* @__PURE__ */ u("main", {
					id: "main-content",
					tabIndex: -1,
					children: fatalError ? /* @__PURE__ */ u(ConnectionError, {
						message: fatalError,
						onRetry: () => setRetry((value) => value + 1),
						onConfigure: () => setConnectionOpen(true)
					}) : appPhase.value === "booting" ? /* @__PURE__ */ u(LoadingView, {}) : view
				})]
			}),
			/* @__PURE__ */ u("nav", {
				class: "mobile-nav",
				"aria-label": t("Primary navigation"),
				children: navigation.map((item) => /* @__PURE__ */ k$1(NavButton, {
					...item,
					key: item.route
				}))
			}),
			/* @__PURE__ */ u(ToastRegion, {}),
			/* @__PURE__ */ u(ConnectionDialog, {
				open: connectionOpen,
				preferences,
				activeKind: activeClientKind.value,
				activeLabel: activeClientLabel.value,
				onClose: () => setConnectionOpen(false),
				onApply: saveConnectionPreferences
			}),
			/* @__PURE__ */ u(Modal, {
				open: updateReady,
				title: t("Dashboard update ready"),
				description: t("A new bundled web interface is available from the running RayanLamp executable."),
				onClose: () => setUpdateReady(false),
				children: /* @__PURE__ */ u("div", {
					class: "modal-actions",
					children: [/* @__PURE__ */ u(Button, {
						kind: "quiet",
						onClick: () => setUpdateReady(false),
						children: t("Later")
					}), /* @__PURE__ */ u(Button, {
						kind: "primary",
						onClick: () => {
							setUpdateReady(false);
							applyUpdate?.(true);
						},
						children: t("Reload dashboard")
					})]
				})
			})
		]
	});
}
function NavButton({ route: target, label, icon }) {
	const current = route.value === target;
	return /* @__PURE__ */ u("button", {
		type: "button",
		class: `nav-button${current ? " is-current" : ""}`,
		"aria-current": current ? "page" : void 0,
		onClick: () => navigate(target),
		children: [/* @__PURE__ */ u(Icon, { name: icon }), /* @__PURE__ */ u("span", { children: t(label) })]
	});
}
function navigate(target) {
	const hash = target === "dashboard" ? "#/" : `#/${target}`;
	if (window.location.hash !== hash) window.location.hash = hash;
	route.value = target;
	window.scrollTo({
		top: 0,
		behavior: "smooth"
	});
	window.setTimeout(() => document.querySelector("#main-content")?.focus(), 0);
}
function routeFromHash(hash) {
	const segment = hash.replace(/^#\/?/, "").split("/")[0].toLowerCase();
	return navigation.some((item) => item.route === segment) ? segment : "dashboard";
}
function LoadingView() {
	return /* @__PURE__ */ u("div", {
		class: "loading-view",
		role: "status",
		children: [
			/* @__PURE__ */ u("div", {
				class: "loading-orbit",
				"aria-hidden": "true",
				children: /* @__PURE__ */ u(Icon, {
					name: "lamp",
					size: 36
				})
			}),
			/* @__PURE__ */ u("h1", { children: t("Connecting to Rayan Lamp") }),
			/* @__PURE__ */ u("p", { children: t("Selecting a transport and synchronizing confirmed lamp state…") })
		]
	});
}
function ConnectionError({ message, onRetry, onConfigure }) {
	return /* @__PURE__ */ u("div", {
		class: "connection-error",
		role: "alert",
		children: [
			/* @__PURE__ */ u("span", {
				class: "error-illustration",
				children: /* @__PURE__ */ u(Icon, {
					name: "warning",
					size: 42
				})
			}),
			/* @__PURE__ */ u("span", {
				class: "eyebrow",
				children: t("Selected connection unavailable")
			}),
			/* @__PURE__ */ u("h1", { children: t("The dashboard could not reach its controller") }),
			/* @__PURE__ */ u("p", { children: t(message) }),
			/* @__PURE__ */ u("p", {
				class: "error-help",
				children: t("Coordinator mode needs the RayanLamp Go service. Direct mode needs this page served over HTTP and browser access to the lamp origin.")
			}),
			/* @__PURE__ */ u("div", {
				class: "connection-error-actions",
				children: [/* @__PURE__ */ u(Button, {
					kind: "quiet",
					icon: /* @__PURE__ */ u(Icon, { name: "network" }),
					onClick: onConfigure,
					children: t("Connection settings")
				}), /* @__PURE__ */ u(Button, {
					kind: "primary",
					icon: /* @__PURE__ */ u(Icon, { name: "reconnect" }),
					onClick: onRetry,
					children: t("Try again")
				})]
			})
		]
	});
}
function themeLabel(value) {
	return t(value === "system" ? "System" : value === "light" ? "Light" : "Dark");
}
//#endregion
//#region src/main.tsx
var root = document.getElementById("app");
if (!root) throw new Error("Rayan Lamp web root is missing.");
browserConsole.start({
	source: "bundled-web",
	action: buildEdition
});
applyWebLocale(document.documentElement.dataset.locale);
R(/* @__PURE__ */ u(App, {}), root);
//#endregion

//# sourceMappingURL=index-P8wU9w4v.js.map