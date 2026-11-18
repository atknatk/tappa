package mt.taptime.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.text.BasicText
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

// The scaffold's one screen: the name and nothing else. Activation (M16-12) and the tap screen
// (M16-13) replace it. Foundation only -- BasicText, no material3 (Q36); the brand's own
// components are drawn by hand when the screens arrive (skill tappa-brand).
class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        setContent { Placeholder() }
    }
}

// Palette tokens, verbatim from skill tappa-brand (res/values/colors.xml carries the same).
private val Ink = Color(0xFF152219)
private val Porcelain = Color(0xFFEDF0EA)
private val TappaGreen = Color(0xFF1F5C41)

@Composable
private fun Placeholder() {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(Porcelain)
            .safeDrawingPadding()
            .padding(24.dp),
        verticalArrangement = Arrangement.Center,
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        BasicText(
            text = stringResource(R.string.app_name),
            style = TextStyle(color = Ink, fontSize = 32.sp, fontWeight = FontWeight.Bold),
        )
        BasicText(
            text = stringResource(R.string.tagline),
            style = TextStyle(color = TappaGreen, fontSize = 14.sp, fontFamily = FontFamily.Monospace),
        )
    }
}
