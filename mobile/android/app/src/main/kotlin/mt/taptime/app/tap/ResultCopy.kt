package mt.taptime.app.tap

import mt.taptime.app.wire.Direction

/**
 * The result screen's words, chosen the way the browser chooses them (web/templates/pages/
 * result.templ, view.go's Headline): the HEADLINE from the verdict and the direction, the CLOSING
 * from the verdict once, the brand line only on a counted tap that is not practice. Each line is a
 * strings.xml key ([Line.key]); TapCopyTest holds every one to the browser's text.
 *
 * NO BUTTON (CLAUDE.md §9): the next action is a new physical touch of the plaque. The app's
 * closing says "close the app" where the browser says "close this page" -- the one adaptation,
 * shared with the activation screens.
 */
object ResultCopy {
    enum class Line(val key: String) {
        CLOCKED_IN("result_clocked_in"),
        CLOCKED_OUT("result_clocked_out"),
        TAPPED("result_tapped"),
        ALREADY("result_already"),
        NOT_COUNTED("result_not_counted"),
        PRACTICE("result_practice"),
        BRAND_RESTAURANT_IN("brand_restaurant_in"),
        BRAND_RESTAURANT_OUT("brand_restaurant_out"),
        BRAND_PRODUCTION_IN("brand_production_in"),
        BRAND_PRODUCTION_OUT("brand_production_out"),
        BRAND_OTHER_IN("brand_other_in"),
        BRAND_OTHER_OUT("brand_other_out"),
        BRAND_NO_DIRECTION("brand_no_direction"),
        ALL_DONE("result_all_done"),
        CLOSE("result_close"),
        IGNORED("result_ignored"),
        REJECTED("result_rejected"),
    }

    /** view.go's ResultView.Headline. */
    fun headline(verdict: String, direction: String): Line = when (verdict) {
        "ok", "flag" -> when (Direction.of(direction)) {
            Direction.IN -> Line.CLOCKED_IN
            Direction.OUT -> Line.CLOCKED_OUT
            else -> Line.TAPPED
        }
        "ignored" -> Line.ALREADY
        "reject" -> Line.NOT_COUNTED
        else -> Line.TAPPED
    }

    /** result.templ's closing (and brandMessage, on ok). A verdict nobody anticipated is reject's. */
    fun closing(verdict: String, direction: String, businessType: String, practice: Boolean): List<Line> = when (verdict) {
        "ok" -> listOfNotNull(brandLine(direction, businessType, practice), Line.ALL_DONE)
        "flag" -> listOf(Line.CLOSE)
        "ignored" -> listOf(Line.IGNORED, Line.CLOSE)
        else -> listOf(Line.REJECTED, Line.CLOSE)
    }

    /** result.templ's brandMessage: keyed on the business TYPE, never shown on a practice tap. */
    fun brandLine(direction: String, businessType: String, practice: Boolean): Line? {
        if (practice) return null
        return when (Direction.of(direction)) {
            Direction.IN -> when (businessType) {
                "restaurant" -> Line.BRAND_RESTAURANT_IN
                "production" -> Line.BRAND_PRODUCTION_IN
                else -> Line.BRAND_OTHER_IN
            }
            Direction.OUT -> when (businessType) {
                "restaurant" -> Line.BRAND_RESTAURANT_OUT
                "production" -> Line.BRAND_PRODUCTION_OUT
                else -> Line.BRAND_OTHER_OUT
            }
            else -> Line.BRAND_NO_DIRECTION
        }
    }
}
