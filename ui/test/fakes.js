export class FakeElement {
  constructor(id = "") {
    this.id = id;
    this.style = {};
    this.children = [];
    this.listeners = {};
    this.classList = {
      toggle: (name, value) => {
        this[name] = Boolean(value);
      },
      add: (...names) =>
        names.forEach((name) => {
          this[name] = true;
        }),
      remove: (...names) =>
        names.forEach((name) => {
          this[name] = false;
        }),
    };
    this.value = "";
    this.textContent = "";
    this.innerText = "";
    this.options = this.children;
    this.files = [];
  }
  addEventListener(type, handler) {
    (this.listeners[type] ||= []).push(handler);
  }
  dispatch(type, event = {}) {
    for (const handler of this.listeners[type] || [])
      handler({ target: this, preventDefault: () => {}, ...event });
  }
  append(...items) {
    this.children.push(...items);
  }
  appendChild(item) {
    this.children.push(item);
    return item;
  }
  replaceChildren(...items) {
    this.children = items;
    this.options = this.children;
  }
  remove() {
    this.removed = true;
  }
  querySelectorAll(selector) {
    return selector === ".session-tab"
      ? this.children.filter((item) => item.className === "session-tab")
      : [];
  }
  insertBefore(item, before) {
    const index = this.children.indexOf(before);
    if (index < 0) this.children.push(item);
    else this.children.splice(index, 0, item);
  }
  setAttribute(name, value) {
    this[name] = value;
  }
  focus() {
    this.focused = true;
  }
  click() {
    this.dispatch("click");
  }
}

export function installDom(ids = []) {
  const elements = new Map(ids.map((id) => [id, new FakeElement(id)]));
  const document = {
    body: new FakeElement("body"),
    documentElement: new FakeElement("html"),
    getElementById: (id) => elements.get(id) || null,
    createElement: (tag) => {
      const element = new FakeElement();
      element.tagName = tag;
      return element;
    },
    createDocumentFragment: () => new FakeElement("fragment"),
  };
  globalThis.document = document;
  const windowListeners = {};
  globalThis.window = {
    location: { search: "", pathname: "/bash", protocol: "http:", hostname: "localhost", port: "" },
    parent: null,
    listeners: windowListeners,
    addEventListener: (type, handler) => {
      (windowListeners[type] ||= []).push(handler);
    },
    dispatch: (type, event) => {
      for (const handler of windowListeners[type] || []) handler(event);
    },
    postMessage: () => {},
  };
  window.parent = window;
  return { document, elements };
}

export function addElements(ids) {
  for (const id of ids)
    globalThis.document.getElementById = ((previous) => (value) =>
      value === id ? (globalThis.document[`_${id}`] ||= new FakeElement(id)) : previous(value))(
      globalThis.document.getElementById,
    );
  return ids.map((id) => globalThis.document[`_${id}`]);
}
