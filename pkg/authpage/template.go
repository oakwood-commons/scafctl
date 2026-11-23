// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authpage

const blueprintTemplateHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.PageTitle}}</title>{{if .UseLogo}}
  <link rel="icon" href="{{.FaviconURI}}">{{end}}
  <style>
    :root { color-scheme: dark; --navy:#071e3d; --cyan:#00e9f2; --mint:#a4f6c5; --muted:#9bafc4; --accent:var(--mint); }
    * { box-sizing:border-box; }
    body { margin:0; min-height:100svh; background:#05152a; color:#eff7ff; font-family:system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif; display:grid; place-items:center; padding:64px 32px; }
    body::before { content:""; position:fixed; inset:0; pointer-events:none; background:radial-gradient(ellipse at 68% 45%,#0b385044,transparent 65%); }
    .sheet { position:relative; width:min(1080px,100%); margin-inline:auto; background:linear-gradient(115deg,#071e3df5,#061b30f5); border:1px solid #43667b55; padding:48px 56px 24px; overflow:hidden; }
    .sheet::before { content:""; position:absolute; inset:0; pointer-events:none; background-image:linear-gradient(#63a2bb09 1px,transparent 1px),linear-gradient(90deg,#63a2bb09 1px,transparent 1px),linear-gradient(#63a2bb0b 1px,transparent 1px),linear-gradient(90deg,#63a2bb0b 1px,transparent 1px); background-size:24px 24px,24px 24px,120px 120px,120px 120px; }
    header,main,footer { position:relative; }
    header { display:flex; align-items:center; justify-content:space-between; gap:24px; }
    .logo { width:220px; height:auto; display:block; margin-left:-9px; }
    .brand-wordmark { font-size:26px; font-weight:700; letter-spacing:-0.8px; color:#ffffff; line-height:1.1; }
    .mono { font-family:ui-monospace,SFMono-Regular,Consolas,monospace; }
    .edition { font-size:10px; letter-spacing:2px; color:#7894af; }
    main { display:grid; grid-template-columns:1.1fr 1fr; align-items:center; gap:20px; min-height:370px; padding:32px 0 44px; }
    .eyebrow { display:flex; align-items:center; gap:10px; text-transform:uppercase; letter-spacing:2px; color:var(--accent); font-size:11px; line-height:1.6; }
    .dot { width:6px; height:6px; flex-shrink:0; border-radius:50%; background:var(--accent); }
    h1 { font-size:clamp(42px,5.2vw,62px); line-height:1.06; letter-spacing:-2.5px; font-weight:550; margin:26px 0 24px; }
    .description { color:#cbdae8; font-size:16px; line-height:1.7; max-width:360px; margin:0; }
    .close { color:var(--muted); font-size:12px; line-height:1.7; margin:22px 0 0; }
    .diagram { width:100%; max-width:400px; justify-self:end; overflow:visible; }
    .construction { fill:none; stroke:#4e8298; stroke-width:.65; opacity:.45; }
    .flow { fill:none; stroke:var(--cyan); stroke-width:2; stroke-linecap:round; stroke-linejoin:round; stroke-dasharray:600; stroke-dashoffset:0; animation:draw 1.3s ease-out both; }
    .diagram text { font-family:ui-monospace,SFMono-Regular,Consolas,monospace; fill:#8aa7be; font-size:8px; letter-spacing:1.5px; }
    .status { fill:var(--accent); }
    .status-mark { fill:none; stroke:#062339; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
    footer { border-top:1px solid #52758e40; padding-top:20px; display:flex; align-items:center; justify-content:space-between; gap:18px; color:#809ab3; font-size:10px; letter-spacing:1px; line-height:1.6; }
    .footer-state { color:var(--accent); }
    body.failure { --accent:#ffc28e; }
    @keyframes draw { from { stroke-dashoffset:600; } to { stroke-dashoffset:0; } }
    @media(prefers-reduced-motion:reduce) { *,*::before,*::after { animation:none!important; transition:none!important; } }
    @media(max-width:700px) { body { padding:28px 18px; } .sheet { padding:26px 26px 20px; } .logo { width:192px; } .brand-wordmark { font-size:22px; } .edition { display:none; } main { grid-template-columns:1fr; gap:16px; padding:30px 0; } h1 { font-size:48px; } .diagram { width:260px; justify-self:center; } footer { font-size:9px; } }
    @media(max-width:380px) { h1 { font-size:40px; } .sheet { padding-inline:20px; } }
  </style>
</head>
<body{{if .IsFailure}} class="failure"{{end}}>
  <div style="width:100%">
    <article class="sheet" aria-labelledby="heading">
      <header>
        {{if .UseLogo}}{{.InlineLogo}}{{else}}<span class="brand-wordmark">{{.BinaryName}}</span>{{end}}
        <span class="edition mono">{{.Edition}}</span>
      </header>
      <main>
        <section>
          <div class="eyebrow mono"><span class="dot" aria-hidden="true"></span><span id="status">{{.StatusText}}</span></div>
          <h1 id="heading">{{.HeadingLine1}}<br>{{.HeadingLine2}}</h1>
          <p class="description" id="description">{{.Description}}</p>
          <p class="close mono" id="guidance">{{.Guidance}}</p>
        </section>
        <svg class="diagram" viewBox="0 0 360 300" aria-hidden="true">
          <g class="construction">
            <path d="M42 25V272M318 25V272M24 50H336M24 239H336M180 12V282" stroke-dasharray="3 5"/>
            <path d="M85 17H275M85 12V22M275 12V22M336 65V221M331 65H341M331 221H341M30 38H54M42 26V50M306 251H330M318 239V263"/>
            <circle cx="180" cy="142" r="112" stroke-dasharray="2 6"/>
          </g>
          <g class="flow">
            <path d="M160 48H190L204 62V100H156V52Q156 48 160 48ZM190 48V62H204M180 101V159M90 178V164Q90 159 96 159H264Q270 159 270 165V178M180 159V178"/>
            <rect x="66" y="178" width="48" height="48" rx="7"/>
            <rect x="156" y="178" width="48" height="48" rx="7"/>
            <rect x="246" y="178" width="48" height="48" rx="7"/>
            <ellipse cx="90" cy="195" rx="10" ry="4"/>
            <path d="M80 195V209C80 215 100 215 100 209V195M80 202C80 208 100 208 100 202M172 195L179 202L172 209M182 195L189 202L182 209"/>
            <path d="M265 193L279 202L265 211Z"/>
          </g>
          <circle class="status" cx="209" cy="100" r="10"/>
          <path class="status-mark" id="status-mark" d="{{.StatusMarkD}}"/>
          <text x="90" y="248" text-anchor="middle">DATA</text>
          <text x="180" y="248" text-anchor="middle">TRANSFORM</text>
          <text x="270" y="248" text-anchor="middle">EXECUTE</text>
          <text x="180" y="35" text-anchor="middle">CONFIGURATION FLOW</text>
        </svg>
      </main>
      <footer class="mono"><span>DECLARATIVE BY DESIGN.</span><span class="footer-state" id="footer-state">{{.FooterState}}</span></footer>
    </article>
  </div>
</body>
</html>
`
