const form = document.getElementById("highlight-form");
const statusEl = document.getElementById("status");
const outputEl = document.getElementById("output");

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  const githubUrl = document.getElementById("github-url").value.trim();
  if (!githubUrl) {
    statusEl.textContent = "Enter a GitHub URL.";
    return;
  }

  statusEl.textContent = "Highlighting...";
  outputEl.innerHTML = "";

  try {
    const response = await fetch(window.APP_CONFIG.apiUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ githubUrl })
    });
    const payload = await response.json();
    if (!response.ok) {
      throw new Error(payload.error || "Request failed");
    }
    outputEl.innerHTML = payload.html;
    statusEl.textContent = "Done.";
  } catch (error) {
    statusEl.textContent = error.message;
  }
});
