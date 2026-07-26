package script

import "github.com/dop251/goja"

// expectJS 精简 chai BDD 子集：pm.expect(v).to.equal(x)/eql/include/have.property/
// be.a/be.ok/be.true/be.false/be.null/be.undefined/be.above/be.below/have.lengthOf，
// 支持 .not 取反。断言失败抛 Error，由 pm.test 捕获为失败。
const expectJS = `
(function () {
  function fmt(v) {
    try { return JSON.stringify(v); } catch (e) { return String(v); }
  }
  function deepEqual(a, b) {
    if (a === b) return true;
    if (typeof a !== typeof b) return false;
    if (a === null || b === null || typeof a !== 'object') return false;
    var ka = Object.keys(a), kb = Object.keys(b);
    if (ka.length !== kb.length) return false;
    for (var i = 0; i < ka.length; i++) {
      if (!deepEqual(a[ka[i]], b[ka[i]])) return false;
    }
    return true;
  }
  function Assertion(value, negated) {
    this._v = value;
    this._n = !!negated;
  }
  Assertion.prototype._check = function (pass, msg) {
    if (this._n) pass = !pass;
    if (!pass) throw new Error((this._n ? 'expected not: ' : 'expected: ') + msg);
  };
  Object.defineProperty(Assertion.prototype, 'to', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'be', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'been', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'is', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'that', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'and', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'have', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'with', { get: function () { return this; } });
  Object.defineProperty(Assertion.prototype, 'not', {
    get: function () { return new Assertion(this._v, !this._n); }
  });
  Object.defineProperty(Assertion.prototype, 'ok', {
    get: function () { this._check(!!this._v, fmt(this._v) + ' to be truthy'); return this; }
  });
  Object.defineProperty(Assertion.prototype, 'true', {
    get: function () { this._check(this._v === true, fmt(this._v) + ' to be true'); return this; }
  });
  Object.defineProperty(Assertion.prototype, 'false', {
    get: function () { this._check(this._v === false, fmt(this._v) + ' to be false'); return this; }
  });
  Object.defineProperty(Assertion.prototype, 'null', {
    get: function () { this._check(this._v === null, fmt(this._v) + ' to be null'); return this; }
  });
  Object.defineProperty(Assertion.prototype, 'undefined', {
    get: function () { this._check(this._v === undefined, fmt(this._v) + ' to be undefined'); return this; }
  });
  Object.defineProperty(Assertion.prototype, 'empty', {
    get: function () {
      var v = this._v;
      var empty = v == null ||
        (typeof v === 'string' && v.length === 0) ||
        (Array.isArray(v) && v.length === 0) ||
        (typeof v === 'object' && Object.keys(v).length === 0);
      this._check(empty, fmt(v) + ' to be empty');
      return this;
    }
  });
  Assertion.prototype.equal = function (x) {
    this._check(this._v === x, fmt(this._v) + ' to equal ' + fmt(x));
    return this;
  };
  Assertion.prototype.eql = function (x) {
    this._check(deepEqual(this._v, x), fmt(this._v) + ' to deeply equal ' + fmt(x));
    return this;
  };
  Assertion.prototype.include = function (x) {
    var v = this._v, pass;
    if (typeof v === 'string') pass = v.indexOf(x) !== -1;
    else if (Array.isArray(v)) pass = v.some(function (it) { return deepEqual(it, x); });
    else if (v && typeof v === 'object') {
      pass = Object.keys(x || {}).every(function (k) { return deepEqual(v[k], x[k]); });
    } else pass = false;
    this._check(pass, fmt(v) + ' to include ' + fmt(x));
    return this;
  };
  Assertion.prototype.contain = Assertion.prototype.include;
  Assertion.prototype.property = function (name, value) {
    var has = this._v != null && Object.prototype.hasOwnProperty.call(this._v, name);
    if (arguments.length > 1) {
      this._check(has && deepEqual(this._v[name], value),
        fmt(this._v) + ' to have property ' + fmt(name) + ' = ' + fmt(value));
    } else {
      this._check(has, fmt(this._v) + ' to have property ' + fmt(name));
    }
    return this;
  };
  Assertion.prototype.lengthOf = function (n) {
    var len = this._v == null ? NaN : this._v.length;
    this._check(len === n, 'length ' + len + ' to be ' + n);
    return this;
  };
  Assertion.prototype.a = function (type) {
    var actual = Array.isArray(this._v) ? 'array' : (this._v === null ? 'null' : typeof this._v);
    this._check(actual === type, fmt(this._v) + ' to be a ' + type + ' (got ' + actual + ')');
    return this;
  };
  Assertion.prototype.an = Assertion.prototype.a;
  Assertion.prototype.above = function (n) {
    this._check(this._v > n, fmt(this._v) + ' to be above ' + n);
    return this;
  };
  Assertion.prototype.below = function (n) {
    this._check(this._v < n, fmt(this._v) + ' to be below ' + n);
    return this;
  };
  Assertion.prototype.least = function (n) {
    this._check(this._v >= n, fmt(this._v) + ' to be at least ' + n);
    return this;
  };
  Assertion.prototype.most = function (n) {
    this._check(this._v <= n, fmt(this._v) + ' to be at most ' + n);
    return this;
  };
  Assertion.prototype.oneOf = function (list) {
    var v = this._v;
    this._check(Array.isArray(list) && list.some(function (x) { return deepEqual(x, v); }),
      fmt(v) + ' to be one of ' + fmt(list));
    return this;
  };
  Assertion.prototype.match = function (re) {
    this._check(re instanceof RegExp && re.test(String(this._v)),
      fmt(this._v) + ' to match ' + String(re));
    return this;
  };
  return function expect(v) { return new Assertion(v, false); };
})()
`

// injectExpect 把 expect 挂到 pm 上（pm.expect）
func injectExpect(vm *goja.Runtime, pm *goja.Object) error {
	v, err := vm.RunString(expectJS)
	if err != nil {
		return err
	}
	return pm.Set("expect", v)
}
