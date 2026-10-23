// Integration snippet for the customer's existing wrapper APK.
// Replace the example website and scheme with your own, then rebuild/sign APK.
package com.example.wrapper

import android.app.Activity
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient

class MainActivity : Activity() {
    private lateinit var webView: WebView
    private val origin = "https://customer.example.com"

    override fun onCreate(state: Bundle?) {
        super.onCreate(state)
        webView = WebView(this)
        webView.settings.javaScriptEnabled = true
        webView.settings.domStorageEnabled = true
        webView.settings.allowFileAccess = false
        webView.settings.allowContentAccess = false
        webView.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                return request.url.scheme != "https" || request.url.host != "customer.example.com"
            }
        }
        setContentView(webView)
        openSite(intent)
    }
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        openSite(intent)
    }
    private fun openSite(intent: Intent) {
        val incoming = intent.data
        val token = if (incoming?.scheme == "customerapp" && incoming.host == "open") incoming.getQueryParameter("d1_token") else null
        val target = Uri.parse(origin).buildUpon()
        if (token != null && Regex("^[a-f0-9]{32}\\.[a-f0-9]{64}$").matches(token)) target.appendQueryParameter("d1_token", token)
        webView.loadUrl(target.build().toString())
    }
    override fun onDestroy() { webView.destroy(); super.onDestroy() }
}
