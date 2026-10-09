export const copyScript = `document.addEventListener("click", (event) => {
  const button = event.target.closest("[data-copy]");
  if (!button) return;
  const source = document.getElementById(button.getAttribute("data-copy"));
  if (!source) return;
  navigator.clipboard.writeText(source.value).then(() => {
    button.textContent = "已复制";
  }, () => {
    button.textContent = "没复制成";
  });
});
`;
