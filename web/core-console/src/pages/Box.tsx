import { useEffect, useRef } from "react";
import template from "../box/template.html?raw";
import { mountBox } from "../box/controller.js";
import "../box/box.css";

export default function Box() {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = root.current;
    if (!element) return;
    // This page owns its DOM; parent renders (e.g. viewport changes) must not
    // replace the controller's content, draft fields or event handlers.
    element.innerHTML = template;
    const dispose = mountBox(element);
    return () => {
      dispose();
      element.replaceChildren();
    };
  }, []);
  return <div ref={root} className="box" />;
}
