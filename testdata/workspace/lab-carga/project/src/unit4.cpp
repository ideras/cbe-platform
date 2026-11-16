#include <algorithm>
#include <array>
#include <cmath>
#include <map>
#include <numeric>
#include <set>
#include <sstream>
#include <string>
#include <unordered_map>
#include <vector>

#include "units.hpp"

namespace {

template <typename T, std::size_t N>
struct Stats4 {
    std::array<T, N> values{};
    T total() const { return std::accumulate(values.begin(), values.end(), T{}); }
    T largest() const { return *std::max_element(values.begin(), values.end()); }
    double mean() const { return static_cast<double>(total()) / N; }
    double deviation() const {
        double m = mean(), sum = 0;
        for (const T& v : values) sum += (v - m) * (v - m);
        return std::sqrt(sum / N);
    }
};

template <typename Key, typename Value>
std::vector<std::pair<Key, Value>> sorted_by_value4(const std::map<Key, Value>& in) {
    std::vector<std::pair<Key, Value>> out(in.begin(), in.end());
    std::sort(out.begin(), out.end(), [](const auto& a, const auto& b) { return a.second > b.second || (a.second == b.second && a.first < b.first); });
    return out;
}

}  // namespace

std::string unit4(int seed) {
    Stats4<long, 64> stats;
    std::map<int, long> histogram;
    std::unordered_map<std::string, int> words;
    std::set<long> seen;
    for (std::size_t k = 0; k < stats.values.size(); ++k) {
        stats.values[k] = (static_cast<long>(seed) * 31 + static_cast<long>(k) * 11) % 1009;
        histogram[static_cast<int>(stats.values[k] % 17)] += stats.values[k];
        seen.insert(stats.values[k]);
        words["w" + std::to_string(stats.values[k] % 23)] += 1;
    }
    auto ranking = sorted_by_value4(histogram);
    std::ostringstream out;
    out << "unit4: total=" << stats.total() << " largest=" << stats.largest() << " mean=" << stats.mean()
        << " deviation=" << stats.deviation() << " distinct=" << seen.size() << " words=" << words.size()
        << " first=" << ranking.front().first;
    return out.str();
}
